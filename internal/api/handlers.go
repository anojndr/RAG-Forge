// Package api provides HTTP handlers for the web search extraction API.
package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
	"web-search-api-for-llms/internal/browser"
	"web-search-api-for-llms/internal/cache"
	"web-search-api-for-llms/internal/config"
	"web-search-api-for-llms/internal/extractor"
	"web-search-api-for-llms/internal/searxng"
	"web-search-api-for-llms/internal/worker"

	jsoniter "github.com/json-iterator/go"
)

var json = jsoniter.ConfigCompatibleWithStandardLibrary

// GetJsoniter exposes the jsoniter instance for other packages (like main.go healthcheck)
func GetJsoniter() jsoniter.API {
	return json
}

// ... (Payload structs remain the same) ...

type RequestPayload struct {
	Query         string `json:"query"`
	MaxResults    int    `json:"max_results"`
	MaxCharPerURL *int   `json:"max_char_per_url,omitempty"`
}
type FinalResponsePayload struct {
	QueryDetails struct {
		Query               string `json:"query"`
		MaxResultsRequested int    `json:"max_results_requested"`
		ActualResultsFound  int    `json:"actual_results_found"`
	} `json:"query_details"`
	Results []extractor.ExtractedResult `json:"results"`
	Error   string                      `json:"error,omitempty"`
}
type ExtractRequestPayload struct {
	URLs          []string `json:"urls"`
	MaxCharPerURL *int     `json:"max_char_per_url,omitempty"`
}
type ExtractResponsePayload struct {
	RequestDetails struct {
		URLsRequested int `json:"urls_requested"`
		URLsProcessed int `json:"urls_processed"`
	} `json:"request_details"`
	Results []extractor.ExtractedResult `json:"results"`
	Error   string                      `json:"error,omitempty"`
}

// SearchHandler holds dependencies for the search handler.
type SearchHandler struct {
	Config          *config.AppConfig
	SearxNGClient   *searxng.Client
	Cache           cache.Cache
	HTTPWorkerPool    *worker.WorkerPool // For lightweight jobs
	BrowserWorkerPool *worker.WorkerPool // For heavyweight jobs
}

// NewSearchHandler creates a new SearchHandler with its dependencies.
func NewSearchHandler(
	appConfig *config.AppConfig,
	browserPool *browser.Pool,
	client *http.Client,
	appCache cache.Cache,
	httpWorkerPool *worker.WorkerPool,
	browserWorkerPool *worker.WorkerPool,
) *SearchHandler {
	return &SearchHandler{
		Config:          appConfig,
		SearxNGClient:   searxng.NewClient(appConfig, client),
		Cache:           appCache,
		HTTPWorkerPool:    httpWorkerPool,
		BrowserWorkerPool: browserWorkerPool,
	}
}

func (sh *SearchHandler) HandleSearch(w http.ResponseWriter, r *http.Request) {
	sh.processRequest(w, r, "/search")
}

func (sh *SearchHandler) HandleExtract(w http.ResponseWriter, r *http.Request) {
	sh.processRequest(w, r, "/extract")
}

// isBrowserJob determines if a URL requires the heavyweight browser worker pool.
func isBrowserJob(url, endpoint string) bool {
	if endpoint == "/extract" {
		return true // All /extract jobs are considered heavy
	}
	// For /search, only Twitter is heavy. Others use lightweight scrapers.
	return strings.Contains(url, "twitter.com") || strings.Contains(url, "x.com")
}

func (sh *SearchHandler) processRequest(w http.ResponseWriter, r *http.Request, endpoint string) {
	if r.Method != http.MethodPost {
		http.Error(w, "Only POST method is allowed", http.StatusMethodNotAllowed)
		return
	}

	var urls []string
	var maxChars *int
	var query string
	var maxResults int

	if endpoint == "/search" {
		var reqPayload RequestPayload
		if err := json.NewDecoder(r.Body).Decode(&reqPayload); err != nil {
			http.Error(w, fmt.Sprintf("Invalid request payload: %v", err), http.StatusBadRequest)
			return
		}
		if reqPayload.Query == "" {
			http.Error(w, "Query parameter is required", http.StatusBadRequest)
			return
		}
		maxChars = reqPayload.MaxCharPerURL
		query = reqPayload.Query
		maxResults = reqPayload.MaxResults
		if maxResults <= 0 {
			maxResults = 10
		}

		slog.Info("Handling search request", "query", query, "max_results", maxResults)

		var err error
		searchKey := getSearchCacheKey(query)
		if cachedURLs, found := sh.Cache.GetSearchURLs(r.Context(), searchKey); found {
			slog.Info("Search cache HIT", "query", query)
			urls = cachedURLs
		} else {
			slog.Info("Search cache MISS", "query", query)
			urls, err = sh.SearxNGClient.FetchResults(r.Context(), query, maxResults)
			if err != nil {
				slog.Error("Error fetching results from search engine(s)", "error", err)
				sh.respondWithError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to fetch results from search engine(s): %v", err))
				return
			}
			sh.Cache.Set(r.Context(), searchKey, urls, sh.Config.SearchCacheTTL)
		}
	} else { // "/extract"
		var reqPayload ExtractRequestPayload
		if err := json.NewDecoder(r.Body).Decode(&reqPayload); err != nil {
			http.Error(w, fmt.Sprintf("Invalid request payload: %v", err), http.StatusBadRequest)
			return
		}
		if len(reqPayload.URLs) == 0 {
			http.Error(w, "URLs parameter is required", http.StatusBadRequest)
			return
		}
		const maxURLs = 20
		if len(reqPayload.URLs) > maxURLs {
			http.Error(w, fmt.Sprintf("Too many URLs provided. Maximum allowed: %d", maxURLs), http.StatusBadRequest)
			return
		}
		urls = reqPayload.URLs
		maxChars = reqPayload.MaxCharPerURL
		slog.Info("Handling extract request", "url_count", len(urls))
	}
	defer r.Body.Close()

	// --- Batched Cache Lookup ---
	resultsChan := make(chan *extractor.ExtractedResult, len(urls))
	var wg sync.WaitGroup
	
	cachedResults, uncachedURLs := sh.checkContentCache(r.Context(), urls, maxChars)
	slog.Info("Content cache summary", "total", len(urls), "hits", len(cachedResults), "misses", len(uncachedURLs))

	for _, cachedResult := range cachedResults {
		resultsChan <- cachedResult
	}

	// --- Dispatch uncached URLs to worker pools ---
	for _, targetURL := range uncachedURLs {
		wg.Add(1)
		job := worker.Job{
			URL:        targetURL,
			Endpoint:   endpoint,
			MaxChars:   maxChars,
			ResultChan: make(chan *extractor.ExtractedResult, 1),
			Context:    r.Context(),
		}

		// Choose the correct worker pool
		if isBrowserJob(targetURL, endpoint) {
			slog.Debug("Dispatching to BROWSER worker pool", "url", targetURL)
			sh.BrowserWorkerPool.JobQueue <- job
		} else {
			slog.Debug("Dispatching to HTTP worker pool", "url", targetURL)
			sh.HTTPWorkerPool.JobQueue <- job
		}

		// Fan-in the results
		go func(job worker.Job) {
			defer wg.Done()
			result := <-job.ResultChan
			cacheKey := getContentCacheKey(job.URL, job.MaxChars)
			if result.Error != "" {
				if checkIfErrorIsPermanent(fmt.Errorf(result.Error)) {
					sh.Cache.Set(r.Context(), cacheKey, result, 5*time.Minute)
				}
			} else {
				sh.Cache.Set(r.Context(), cacheKey, result, sh.Config.ContentCacheTTL)
			}
			resultsChan <- result
		}(job)
	}

	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	// --- Aggregate and respond ---
	var finalResults []extractor.ExtractedResult
	for res := range resultsChan {
		finalResults = append(finalResults, *res)
	}

	slog.Info("Finished all extractions", "count", len(finalResults))
	if r.Context().Err() != nil {
		slog.Warn("Context cancelled, not writing response", "path", r.URL.Path)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if endpoint == "/search" {
		resp := FinalResponsePayload{Results: finalResults}
		resp.QueryDetails.Query = query
		resp.QueryDetails.MaxResultsRequested = maxResults
		resp.QueryDetails.ActualResultsFound = len(urls)
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			slog.Error("Error encoding final response", "error", err)
		}
	} else {
		resp := ExtractResponsePayload{Results: finalResults}
		resp.RequestDetails.URLsRequested = len(urls)
		resp.RequestDetails.URLsProcessed = len(finalResults)
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			slog.Error("Error encoding extract response", "error", err)
		}
	}
}

// checkContentCache performs a batched cache lookup.
func (sh *SearchHandler) checkContentCache(ctx context.Context, urls []string, maxChars *int) (
	cachedResults []*extractor.ExtractedResult,
	uncachedURLs []string,
) {
    if len(urls) == 0 {
        return nil, nil
    }

	keysToCheck := make([]string, len(urls))
	urlToCacheKey := make(map[string]string, len(urls))
	for i, u := range urls {
		key := getContentCacheKey(u, maxChars)
		keysToCheck[i] = key
		urlToCacheKey[u] = key
	}

	// Attempt batched fetch if using Redis
	if redisCache, isRedis := sh.Cache.(*cache.RedisCache); isRedis {
		foundMap, err := redisCache.MGetExtractedResults(ctx, keysToCheck)
		if err != nil {
			slog.Warn("Redis MGET failed, falling back to individual gets", "error", err)
			// Fallback to individual gets if MGET fails
			return sh.checkContentCacheIndividually(ctx, urls, maxChars)
		}

		foundKeys := make(map[string]bool)
		for key, result := range foundMap {
			cachedResults = append(cachedResults, result)
			foundKeys[key] = true
		}

		for _, u := range urls {
			key := urlToCacheKey[u]
			if !foundKeys[key] {
				uncachedURLs = append(uncachedURLs, u)
			}
		}
		return cachedResults, uncachedURLs
	}

	// Fallback for in-memory cache or other cache types
	return sh.checkContentCacheIndividually(ctx, urls, maxChars)
}

// checkContentCacheIndividually is the fallback for non-redis or failed MGET
func (sh *SearchHandler) checkContentCacheIndividually(ctx context.Context, urls []string, maxChars *int) (
	cachedResults []*extractor.ExtractedResult,
	uncachedURLs []string,
) {
	for _, u := range urls {
		key := getContentCacheKey(u, maxChars)
		if cachedResult, found := sh.Cache.GetExtractedResult(ctx, key); found {
			cachedResults = append(cachedResults, cachedResult)
		} else {
			uncachedURLs = append(uncachedURLs, u)
		}
	}
	return cachedResults, uncachedURLs
}

func (sh *SearchHandler) respondWithError(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(map[string]string{"error": message}); err != nil {
		slog.Error("Error encoding error response", "error", err)
	}
}

// ... (Helper functions like getSearchCacheKey, getContentCacheKey, checkIfErrorIsPermanent)
func getSearchCacheKey(query string) string { return "search_cache:" + query }
func getContentCacheKey(url string, maxChars *int) string {
	key := "content_cache:" + url
	if maxChars != nil { key = fmt.Sprintf("%s:%d", key, *maxChars) }
	return key
}
func checkIfErrorIsPermanent(err error) bool {
	if err == nil { return false }
	errStr := err.Error()
	return errors.Is(err, extractor.ErrUnsupportedContentType) || errors.Is(err, extractor.ErrNotPDF) ||
		strings.Contains(errStr, "404 Not Found") || strings.Contains(errStr, "410 Gone") ||
		strings.Contains(errStr, "failed to get tweet") || strings.Contains(errStr, "video unavailable") ||
		strings.Contains(errStr, "no such host")
}
