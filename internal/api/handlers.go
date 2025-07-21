// Package api provides HTTP handlers for the web search extraction API.
package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"strings"
	"time"

	"web-search-api-for-llms/internal/browser"
	"web-search-api-for-llms/internal/cache"
	"web-search-api-for-llms/internal/config"
	"web-search-api-for-llms/internal/extractor"
	"web-search-api-for-llms/internal/logger"
	"web-search-api-for-llms/internal/searxng"
	"web-search-api-for-llms/internal/worker"
)
// RequestPayload defines the expected JSON structure for the /search endpoint.
type RequestPayload struct {
	Query         string `json:"query"`
	MaxResults    int    `json:"max_results"`
	MaxCharPerURL *int   `json:"max_char_per_url,omitempty"` // Max chars allowed per URL content, nil means infinite
}

// FinalResponsePayload defines the structure for the API's final response.
type FinalResponsePayload struct {
	QueryDetails struct {
		Query               string `json:"query"`
		MaxResultsRequested int    `json:"max_results_requested"`
		ActualResultsFound  int    `json:"actual_results_found"` // Number of URLs from SearxNG
	} `json:"query_details"`
	Results []extractor.ExtractedResult `json:"results"`
	Error   string                      `json:"error,omitempty"`
}

// ExtractRequestPayload defines the expected JSON structure for the /extract endpoint.
type ExtractRequestPayload struct {
	URLs          []string `json:"urls"`                     // Array of URLs to extract content from
	MaxCharPerURL *int     `json:"max_char_per_url,omitempty"` // Max chars allowed per URL content, nil means infinite
}

// ExtractResponsePayload defines the structure for the /extract endpoint response.
type ExtractResponsePayload struct {
	RequestDetails struct {
		URLsRequested int `json:"urls_requested"`
		URLsProcessed int `json:"urls_processed"`
	} `json:"request_details"`
	Results []extractor.ExtractedResult `json:"results"`
	Error   string                      `json:"error,omitempty"`
}

// getContentCacheKey generates a cache key for content based on the URL and character limit.
func getContentCacheKey(url string, maxChars *int) string {
	if maxChars == nil {
		return "content:" + url + ":full"
	}
	return fmt.Sprintf("content:%s:%d", url, *maxChars)
}

// checkIfErrorIsPermanent checks if an error is likely to be permanent (e.g., 404 Not Found).
func checkIfErrorIsPermanent(err error) bool {
	// In a real-world scenario, this would be more sophisticated.
	// We might check for specific error types or messages.
	if err == nil {
		return false
	}
	// For this example, we'll consider any error permanent.
	// A better implementation would check for specific error strings like "404" or "not found".
	return true
}

// getTaskWeight determines the weight of a task based on the URL and endpoint.
func getTaskWeight(url, endpoint string) int {
	// The /extract endpoint always uses a browser
	if endpoint == "/extract" {
		return 5 // High weight
	}

	// For /search, determine by URL type
	if strings.Contains(url, "twitter.com") || strings.Contains(url, "x.com") {
		return 5 // High weight
	}

	// Default for fast tasks
	return 1
}

// SearchHandler holds dependencies for the search handler and manages HTTP request processing.
type SearchHandler struct {
	Config        *config.AppConfig
	SearxNGClient *searxng.Client
	Dispatcher    *extractor.Dispatcher
	Cache         cache.Cache
	WorkerPool    *worker.WorkerPool
}

// NewSearchHandler creates a new SearchHandler with its dependencies.
func NewSearchHandler(appConfig *config.AppConfig, browserPool *browser.Pool, client *http.Client, appCache cache.Cache, workerPool *worker.WorkerPool) *SearchHandler {
	return &SearchHandler{
		Config:        appConfig,
		SearxNGClient: searxng.NewClient(appConfig, client),
		Dispatcher:    extractor.NewDispatcher(appConfig, browserPool, client),
		Cache:         appCache,
		WorkerPool:    workerPool,
	}
}

// HandleSearch is the actual HTTP handler method for the /search endpoint.
func (sh *SearchHandler) HandleSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Only POST method is allowed", http.StatusMethodNotAllowed)
		return
	}

	var reqPayload RequestPayload
	if err := json.NewDecoder(r.Body).Decode(&reqPayload); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request payload: %v", err), http.StatusBadRequest)
		return
	}
	defer func() {
		if err := r.Body.Close(); err != nil {
			logger.LogError("Error closing request body: %v", err)
		}
	}()

	if reqPayload.Query == "" {
		http.Error(w, "Query parameter is required", http.StatusBadRequest)
		return
	}
	if reqPayload.MaxResults <= 0 {
		log.Println("MaxResults not provided or invalid, defaulting to 10")
		reqPayload.MaxResults = 10
	}

	log.Printf("Handling search request: Query='%s', MaxResults=%d", reqPayload.Query, reqPayload.MaxResults)

	var urls []string
	var err error

	// At the top of HandleSearch
	if cachedURLs, found := sh.Cache.Get(r.Context(), "search:"+reqPayload.Query); found {
		log.Printf("Search cache HIT for query: %s", reqPayload.Query)
		urls = cachedURLs.([]string)
	} else {
		log.Printf("Search cache MISS for query: %s", reqPayload.Query)
		// ... fetch from SearxNG/Serper
		urls, err = sh.SearxNGClient.FetchResults(r.Context(), reqPayload.Query, reqPayload.MaxResults)
		if err != nil {
			logger.LogError("Error fetching results from search engine(s): %v", err)
			resp := FinalResponsePayload{
				Error: fmt.Sprintf("Failed to fetch results from search engine(s): %v", err),
			}
			resp.QueryDetails.Query = reqPayload.Query
			resp.QueryDetails.MaxResultsRequested = reqPayload.MaxResults
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			if err := json.NewEncoder(w).Encode(resp); err != nil {
				logger.LogError("Error encoding error response: %v", err)
			}
			return
		}
		sh.Cache.Set(r.Context(), "search:"+reqPayload.Query, urls, sh.Config.SearchCacheTTL)
	}

	log.Printf("Successfully fetched %d URLs for query '%s'. Starting extraction with unlimited concurrency.", len(urls), reqPayload.Query)

	resultsChan := make(chan *extractor.ExtractedResult, len(urls))
	var wg sync.WaitGroup

	for _, targetURL := range urls {
		// Check cache before dispatching job
		cacheKey := getContentCacheKey(targetURL, reqPayload.MaxCharPerURL)
		if cachedResult, found := sh.Cache.Get(r.Context(), cacheKey); found {
			log.Printf("Content cache HIT for URL: %s", targetURL)
			resultsChan <- cachedResult.(*extractor.ExtractedResult)
			continue
		}

		wg.Add(1)
		job := worker.Job{
			URL:        targetURL,
			Endpoint:   "/search",
			MaxChars:   reqPayload.MaxCharPerURL,
			ResultChan: make(chan *extractor.ExtractedResult, 1),
			Context:    r.Context(),
		}

		sh.WorkerPool.JobQueue <- job

		go func(job worker.Job) {
			defer wg.Done()
			result := <-job.ResultChan
			if result.Error != "" {
				if checkIfErrorIsPermanent(fmt.Errorf(result.Error)) {
					sh.Cache.Set(r.Context(), cacheKey, result, 5*time.Minute) // Short TTL for failures
				}
			} else {
				sh.Cache.Set(r.Context(), cacheKey, result, sh.Config.ContentCacheTTL)
			}
			resultsChan <- result
		}(job)
	}

	// Wait for all jobs to be processed and then close the main results channel
	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	extractedResults := make([]extractor.ExtractedResult, 0, len(urls))
	for res := range resultsChan {
		extractedResults = append(extractedResults, *res)
	}

	log.Printf("Finished all extractions. Aggregated %d results.", len(extractedResults))


	resp := FinalResponsePayload{}
	resp.QueryDetails.Query = reqPayload.Query
	resp.QueryDetails.MaxResultsRequested = reqPayload.MaxResults
	resp.QueryDetails.ActualResultsFound = len(urls) // Number of URLs attempted
	resp.Results = extractedResults

	// Check if the context was cancelled (e.g., by timeout) before writing the response
	if r.Context().Err() != nil {
		log.Printf("Context cancelled for %s, not writing response", r.URL.Path)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		logger.LogError("Error encoding final response: %v", err)
	}
}


// HandleExtract is the HTTP handler method for the /extract endpoint.
func (sh *SearchHandler) HandleExtract(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Only POST method is allowed", http.StatusMethodNotAllowed)
		return
	}

	var reqPayload ExtractRequestPayload
	if err := json.NewDecoder(r.Body).Decode(&reqPayload); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request payload: %v", err), http.StatusBadRequest)
		return
	}
	defer func() {
		if err := r.Body.Close(); err != nil {
			logger.LogError("Error closing request body: %v", err)
		}
	}()

	if len(reqPayload.URLs) == 0 {
		http.Error(w, "URLs parameter is required and must contain at least one URL", http.StatusBadRequest)
		return
	}

	// Limit the number of URLs to prevent abuse
	const maxURLs = 20 // Use const instead of var for immutable values
	if len(reqPayload.URLs) > maxURLs {
		http.Error(w, fmt.Sprintf("Too many URLs provided. Maximum allowed: %d", maxURLs), http.StatusBadRequest)
		return
	}

	log.Printf("Handling extract request for %d URLs with unlimited concurrency", len(reqPayload.URLs))

	resultsChan := make(chan *extractor.ExtractedResult, len(reqPayload.URLs))
	var wg sync.WaitGroup

	for _, targetURL := range reqPayload.URLs {
		// Check cache before dispatching job
		cacheKey := getContentCacheKey(targetURL, reqPayload.MaxCharPerURL)
		if cachedResult, found := sh.Cache.Get(r.Context(), cacheKey); found {
			log.Printf("Content cache HIT for URL: %s", targetURL)
			resultsChan <- cachedResult.(*extractor.ExtractedResult)
			continue
		}

		wg.Add(1)
		job := worker.Job{
			URL:        targetURL,
			Endpoint:   "/extract",
			MaxChars:   reqPayload.MaxCharPerURL,
			ResultChan: make(chan *extractor.ExtractedResult, 1),
			Context:    r.Context(),
		}

		sh.WorkerPool.JobQueue <- job

		go func(job worker.Job) {
			defer wg.Done()
			result := <-job.ResultChan
			if result.Error != "" {
				if checkIfErrorIsPermanent(fmt.Errorf(result.Error)) {
					sh.Cache.Set(r.Context(), cacheKey, result, 5*time.Minute) // Short TTL for failures
				}
			} else {
				sh.Cache.Set(r.Context(), cacheKey, result, sh.Config.ContentCacheTTL)
			}
			resultsChan <- result
		}(job)
	}

	// Wait for all jobs to be processed and then close the main results channel
	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	extractedResults := make([]extractor.ExtractedResult, 0, len(reqPayload.URLs))
	for res := range resultsChan {
		extractedResults = append(extractedResults, *res)
	}
	
	log.Printf("Finished all extractions. Processed %d results.", len(extractedResults))


	resp := ExtractResponsePayload{}
	resp.RequestDetails.URLsRequested = len(reqPayload.URLs)
	resp.RequestDetails.URLsProcessed = len(extractedResults)
	resp.Results = extractedResults

	// Check if the context was cancelled (e.g., by timeout) before writing the response
	if r.Context().Err() != nil {
		log.Printf("Context cancelled for %s, not writing response", r.URL.Path)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		logger.LogError("Error encoding extract response: %v", err)
	}
}


