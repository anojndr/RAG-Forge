package searxng

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"web-search-api-for-llms/internal/config"
	"web-search-api-for-llms/internal/logger"
	"web-search-api-for-llms/internal/useragent"
)

// SearxNGResultItem matches the structure of individual items in SearxNG's JSON output.
type SearxNGResultItem struct {
	URL     string  `json:"url"`
	Title   string  `json:"title"`
	Content string  `json:"content"`
	Score   float64 `json:"score"`
	Engine  string  `json:"engine"`
	// Add other fields if needed, e.g., "category", "publishedDate"
}

// SearxNGResponse matches the top-level structure of SearxNG's JSON output.
type SearxNGResponse struct {
	Query               string              `json:"query"`
	NumberOfResults     int                 `json:"number_of_results"` // This might be total results, not per page.
	Results             []SearxNGResultItem `json:"results"`
	Answers             []json.RawMessage   `json:"answers,omitempty"`     // Using json.RawMessage for fields with variable structure
	Corrections         []json.RawMessage   `json:"corrections,omitempty"` // Or define specific structs if structure is known and needed
	Infoboxes           []json.RawMessage   `json:"infoboxes,omitempty"`
	Suggestions         []string            `json:"suggestions,omitempty"`
	UnresponsiveEngines [][]string          `json:"unresponsive_engines,omitempty"`
}

// SerperOrganicResult defines the structure for a single organic result from Serper API.
type SerperOrganicResult struct {
	Title    string `json:"title"`
	Link     string `json:"link"`
	Snippet  string `json:"snippet"`
	Position int    `json:"position"`
}

// SerperSearchResponse matches the top-level structure of Serper Search API's JSON output.
type SerperSearchResponse struct {
	SearchParameters json.RawMessage       `json:"searchParameters,omitempty"`
	Organic          []SerperOrganicResult `json:"organic"`
	// Add other fields like relatedSearches, peopleAlsoAsk, etc. if needed
}

// optimizedHTTPClient provides a high-performance HTTP client with connection pooling
var optimizedHTTPClient = &http.Client{
	Timeout: 5 * time.Second, // Fast timeout for search APIs
	Transport: &http.Transport{
		MaxIdleConns:        100,              // Maximum idle connections across all hosts
		MaxIdleConnsPerHost: 30,               // Higher limit for search APIs
		IdleConnTimeout:     90 * time.Second, // How long idle connections stay open
		DisableCompression:  false,            // Enable compression for faster transfers
		ForceAttemptHTTP2:   true,             // Use HTTP/2 when possible
	},
}

// Client is an API client for search engines.
type Client struct {
	config *config.AppConfig
}

// NewClient creates a new search client.
func NewClient(appConfig *config.AppConfig) *Client {
	return &Client{
		config: appConfig,
	}
}

// fetchSerperResults fetches search results from the Serper.dev API.
func (c *Client) fetchSerperResults(query string, maxResults int) ([]string, error) {
	if c.config.SerperAPIKey == "" {
		log.Println("Serper API key is not configured. Skipping Serper search.")
		return nil, fmt.Errorf("serper API key not configured")
	}

	apiURL := c.config.SerperAPIURL
	if apiURL == "" {
		return nil, fmt.Errorf("serper API URL not configured")
	}

	// Serper uses 'num' for number of results, but it's often 10, 20, 30, etc.
	// We'll fetch a reasonable amount and then trim if necessary,
	// as Serper might not support arbitrary 'num' values for fine-grained control like '7'.
	// The API docs suggest 'num' defaults to 10. Let's request a bit more if maxResults is high.
	numResultsToRequest := 10
	if maxResults > 10 && maxResults <= 20 {
		numResultsToRequest = 20
	} else if maxResults > 20 {
		numResultsToRequest = 30 // Or adjust as per Serper's typical pagination/result counts
	}

	payload := map[string]interface{}{
		"q":   query,
		"num": numResultsToRequest,
		// Potentially add other params like "gl" (country), "hl" (language)
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("error marshalling Serper request payload: %w", err)
	}

	log.Printf("Fetching Serper API results for query: '%s' from %s with num: %d\n", query, apiURL, numResultsToRequest)

	req, err := http.NewRequest("POST", apiURL, bytes.NewReader(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("error creating Serper API request: %w", err)
	}
	req.Header.Set("X-API-KEY", c.config.SerperAPIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", useragent.Random())

	resp, err := optimizedHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error fetching results from Serper API: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Printf("Warning: failed to close response body: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("serper API request failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var serperResp SerperSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&serperResp); err != nil {
		return nil, fmt.Errorf("error decoding Serper API response: %w", err)
	}

	var urls []string
	if serperResp.Organic != nil {
		for _, item := range serperResp.Organic {
			if item.Link != "" {
				urls = append(urls, item.Link)
				if len(urls) >= maxResults {
					break
				}
			}
		}
	}
	log.Printf("Fetched %d URLs from Serper API.\n", len(urls))
	return urls, nil
}

// fetchSearxNGResults fetches search results from a SearxNG instance with concurrent pagination.
func (c *Client) fetchSearxNGResults(query string, maxResults int) ([]SearxNGResultItem, error) {
	resultsPerPage := 10 // Default assumption for SearxNG
	maxPages := 5        // Maximum pages to fetch concurrently

	log.Printf("Fetching SearxNG results for query: '%s', aiming for %d results with concurrent pagination.\n", query, maxResults)

	// Calculate how many pages we might need
	estimatedPages := (maxResults + resultsPerPage - 1) / resultsPerPage
	if estimatedPages > maxPages {
		estimatedPages = maxPages
	}

	// Create channels for concurrent page fetching
	type pageResult struct {
		page  int
		items []SearxNGResultItem
		err   error
	}

	resultsChan := make(chan pageResult, estimatedPages)
	var wg sync.WaitGroup

	// Fetch pages concurrently
	for page := 1; page <= estimatedPages; page++ {
		wg.Add(1)
		go func(pageNum int) {
			defer wg.Done()

			apiURL, err := url.Parse(c.config.SearxNGURL + "/search")
			if err != nil {
				resultsChan <- pageResult{page: pageNum, err: fmt.Errorf("error parsing SearxNG base URL: %w", err)}
				return
			}

			params := url.Values{}
			params.Add("q", query)
			params.Add("format", "json")
			params.Add("pageno", fmt.Sprintf("%d", pageNum))
			apiURL.RawQuery = params.Encode()

			log.Printf("Fetching page %d from SearxNG: %s\n", pageNum, apiURL.String())

			req, err := http.NewRequest("GET", apiURL.String(), nil)
			if err != nil {
				resultsChan <- pageResult{page: pageNum, err: fmt.Errorf("error creating SearxNG request: %w", err)}
				return
			}
			req.Header.Set("User-Agent", useragent.Random())

			resp, err := optimizedHTTPClient.Do(req)
			if err != nil {
				logger.LogError("Error fetching from SearxNG page %d: %v.\n", pageNum, err)
				resultsChan <- pageResult{page: pageNum, err: err}
				return
			}
			defer func() {
				if err := resp.Body.Close(); err != nil {
					log.Printf("Warning: failed to close response body for page %d: %v", pageNum, err)
				}
			}()

			if resp.StatusCode != http.StatusOK {
				bodyBytes, _ := io.ReadAll(resp.Body)
				logger.LogError("SearxNG request failed with status %d for page %d: %s.\n", resp.StatusCode, pageNum, string(bodyBytes))
				resultsChan <- pageResult{page: pageNum, err: fmt.Errorf("SearxNG request failed with status %d", resp.StatusCode)}
				return
			}

			var searxNGResp SearxNGResponse
			if err := json.NewDecoder(resp.Body).Decode(&searxNGResp); err != nil {
				logger.LogError("Error decoding SearxNG response for page %d: %v.\n", pageNum, err)
				resultsChan <- pageResult{page: pageNum, err: err}
				return
			}

			log.Printf("Fetched %d results from SearxNG page %d\n", len(searxNGResp.Results), pageNum)
			resultsChan <- pageResult{page: pageNum, items: searxNGResp.Results, err: nil}
		}(page)
	}

	// Wait for all goroutines to complete
	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	// Collect and sort results by page number
	pageResults := make(map[int][]SearxNGResultItem)
	var errors []error

	for result := range resultsChan {
		if result.err != nil {
			errors = append(errors, result.err)
			continue
		}
		pageResults[result.page] = result.items
	}

	// If all pages failed, return the first error
	if len(pageResults) == 0 && len(errors) > 0 {
		return nil, errors[0]
	}

	// Combine results in page order
	var allItems []SearxNGResultItem
	for page := 1; page <= estimatedPages; page++ {
		if items, exists := pageResults[page]; exists {
			allItems = append(allItems, items...)
			// Stop if we have enough results
			if len(allItems) >= maxResults*2 && maxResults > 0 {
				log.Printf("Collected enough candidates (%d) from SearxNG, stopping.", len(allItems))
				break
			}
		}
	}

	log.Printf("Total items collected from SearxNG: %d\n", len(allItems))
	return allItems, nil
}

// FetchResults fetches search results based on configured main and fallback engines.
// Results from SearxNG are sorted by score.
func (c *Client) FetchResults(query string, maxResults int) ([]string, error) {
	var urls []string
	var err error

	mainEngine := strings.ToLower(c.config.MainSearchEngine)
	fallbackEngine := strings.ToLower(c.config.FallbackSearchEngine)

	log.Printf("Main search engine: '%s', Fallback search engine: '%s'", mainEngine, fallbackEngine)

	// Try Main Search Engine
	if mainEngine != "" {
		log.Printf("Attempting search with main engine: %s", mainEngine)
		switch mainEngine {
		case "searxng":
			searxngItems, fetchErr := c.fetchSearxNGResults(query, maxResults)
			if fetchErr != nil {
				logger.LogError("Error fetching from main engine (SearxNG): %v", fetchErr)
				err = fetchErr // Store error for potential fallback
			} else if len(searxngItems) > 0 {
				sort.SliceStable(searxngItems, func(i, j int) bool {
					return searxngItems[i].Score > searxngItems[j].Score
				})
				for i := 0; i < len(searxngItems) && i < maxResults; i++ {
					urls = append(urls, searxngItems[i].URL)
				}
				log.Printf("Got %d results from main engine (SearxNG)", len(urls))
			} else {
				log.Printf("Main engine (SearxNG) returned 0 results.")
			}
		case "serper":
			serperURLs, fetchErr := c.fetchSerperResults(query, maxResults)
			if fetchErr != nil {
				logger.LogError("Error fetching from main engine (Serper): %v", fetchErr)
				err = fetchErr // Store error
			} else if len(serperURLs) > 0 {
				urls = serperURLs
				log.Printf("Got %d results from main engine (Serper)", len(urls))
			} else {
				log.Printf("Main engine (Serper) returned 0 results.")
			}
		default:
			log.Printf("Unsupported main search engine configured: %s", mainEngine)
			err = fmt.Errorf("unsupported main search engine: %s", mainEngine)
		}
	} else {
		log.Println("No main search engine configured.")
		// If no main engine, we might proceed directly to fallback or error out.
		// For now, let's assume an error if no main engine is set and we need results.
		err = fmt.Errorf("no main search engine configured")
	}

	// Try Fallback Search Engine if main failed or returned no results
	if (err != nil || len(urls) == 0) && fallbackEngine != "" && fallbackEngine != mainEngine {
		logger.LogError("Main engine failed or returned no results. Attempting fallback to: %s", fallbackEngine)
		var fallbackErr error
		switch fallbackEngine {
		case "searxng":
			searxngItems, fetchErr := c.fetchSearxNGResults(query, maxResults)
			if fetchErr != nil {
				logger.LogError("Error fetching from fallback engine (SearxNG): %v", fetchErr)
				fallbackErr = fetchErr
			} else if len(searxngItems) > 0 {
				urls = []string{} // Clear previous results if any (e.g. main engine error but urls not cleared)
				sort.SliceStable(searxngItems, func(i, j int) bool {
					return searxngItems[i].Score > searxngItems[j].Score
				})
				for i := 0; i < len(searxngItems) && i < maxResults; i++ {
					urls = append(urls, searxngItems[i].URL)
				}
				log.Printf("Got %d results from fallback engine (SearxNG)", len(urls))
				err = nil // Clear previous error as fallback succeeded
			} else {
				log.Printf("Fallback engine (SearxNG) returned 0 results.")
				// If err was already set by main engine, keep it. If main was just empty, set new error.
				if err == nil {
					fallbackErr = fmt.Errorf("fallback engine (SearxNG) returned 0 results")
				}
			}
		case "serper":
			serperURLs, fetchErr := c.fetchSerperResults(query, maxResults)
			if fetchErr != nil {
				logger.LogError("Error fetching from fallback engine (Serper): %v", fetchErr)
				fallbackErr = fetchErr
			} else if len(serperURLs) > 0 {
				urls = serperURLs // Overwrite with fallback results
				log.Printf("Got %d results from fallback engine (Serper)", len(urls))
				err = nil // Clear previous error as fallback succeeded
			} else {
				log.Printf("Fallback engine (Serper) returned 0 results.")
				if err == nil {
					fallbackErr = fmt.Errorf("fallback engine (Serper) returned 0 results")
				}
			}
		default:
			log.Printf("Unsupported fallback search engine configured: %s", fallbackEngine)
			fallbackErr = fmt.Errorf("unsupported fallback search engine: %s", fallbackEngine)
		}
		// If fallback also had an error, and main had an error, prioritize main's error or combine.
		// For now, if fallback fails, the original 'err' (if any) or the new fallbackErr will be used.
		if fallbackErr != nil {
			if err != nil { // If main also failed
				err = fmt.Errorf("main engine failed (%v) and fallback engine also failed (%v)", err, fallbackErr)
			} else { // If main was just empty and fallback failed
				err = fallbackErr
			}
		}

	} else if err == nil && len(urls) == 0 && mainEngine != "" {
		// Main engine succeeded but returned 0 results, and no fallback or fallback is same as main
		err = fmt.Errorf("%s returned 0 results and no different fallback configured", mainEngine)
	}

	if err != nil {
		logger.LogError("Final error after attempting search engines: %v", err)
		return nil, err
	}
	if len(urls) == 0 {
		log.Printf("No results found for query '%s' after attempting configured search engines.", query)
		return nil, fmt.Errorf("no results found for query: %s", query)
	}

	log.Printf("Returning %d top URLs after processing main/fallback engines.\n", len(urls))
	return urls, nil
}
