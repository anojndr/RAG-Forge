package extractor

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"web-search-api-for-llms/internal/browser"
	"web-search-api-for-llms/internal/config"
	"web-search-api-for-llms/internal/logger"
	"web-search-api-for-llms/internal/utils"
)

// Dispatcher is responsible for identifying the type of URL and calling the appropriate extractor.
type Dispatcher struct {
	Config             *config.AppConfig
	BrowserPool        *browser.Pool
	PythonPool         *utils.PythonPool
	mainHTTPClient     *http.Client
	youtubeExtractor   Extractor
	redditExtractor    Extractor
	twitterExtractor   Extractor
	pdfExtractor       Extractor
	webpageExtractor   Extractor
	jsWebpageExtractor Extractor
}

// NewDispatcher creates a new Dispatcher and initializes all concrete extractors.
func NewDispatcher(appConfig *config.AppConfig, browserPool *browser.Pool, pythonPool *utils.PythonPool, client *http.Client) *Dispatcher {
	ytExtractor, err := NewYouTubeExtractor(appConfig, client, pythonPool)
	if err != nil {
		log.Printf("Warning: Failed to initialize YouTubeExtractor: %v. YouTube URLs may not be processed.", err)
		// Depending on desired behavior, you might want to panic or handle this more gracefully.
		// For now, we'll let it proceed with a nil extractor for YouTube.
	}

	rdExtractor := NewRedditExtractor(appConfig, client)
	twExtractor := NewTwitterExtractor(appConfig, browserPool, client)
	pdfExtractor := NewPDFExtractor(appConfig, client)
	wpExtractor := NewWebpageExtractor(appConfig, client)
	jsWpExtractor := NewJSWebpageExtractor(appConfig, browserPool, client)

	return &Dispatcher{
		Config:             appConfig,
		BrowserPool:        browserPool,
		PythonPool:         pythonPool,
		mainHTTPClient:     client,
		youtubeExtractor:   ytExtractor, // This can be nil if NewYouTubeExtractor failed
		redditExtractor:    rdExtractor,
		twitterExtractor:   twExtractor,
		pdfExtractor:       pdfExtractor,
		webpageExtractor:   wpExtractor,
		jsWebpageExtractor: jsWpExtractor,
	}
}

// DispatchAndExtract determines the URL type and calls the appropriate extractor.
func (d *Dispatcher) DispatchAndExtract(targetURL string) (*ExtractedResult, error) {
	// Default to not using headless browser if context is not provided.
	return d.DispatchAndExtractWithContext(targetURL, "", false)
}

// DispatchAndExtractWithContext determines the URL type and calls the appropriate extractor with context.
func (d *Dispatcher) DispatchAndExtractWithContext(targetURL string, endpoint string, useHeadlessBrowser bool) (*ExtractedResult, error) {
	log.Printf("Dispatching URL: %s from endpoint: %s", targetURL, endpoint)

	parsedURL, err := url.Parse(targetURL)
	if err != nil {
		wrappedErr := fmt.Errorf("failed to parse URL %s: %w", targetURL, err)
		logger.LogError("Error: %v", wrappedErr)
		result := &ExtractedResult{
			URL:                   targetURL,
			ProcessedSuccessfully: false,
			Error:                 "Invalid URL format",
			SourceType:            "unknown",
		}
		return result, wrappedErr
	}

	hostname := strings.ToLower(parsedURL.Hostname())

	// 1. Check for YouTube (comprehensive domain check)
	if (strings.Contains(hostname, "youtube.com") ||
		strings.Contains(hostname, "youtu.be") ||
		strings.Contains(hostname, "youtube-nocookie.com") ||
		strings.Contains(hostname, "music.youtube.com") ||
		strings.Contains(hostname, "gaming.youtube.com") ||
		strings.Contains(hostname, "tv.youtube.com") ||
		strings.Contains(hostname, "m.youtube.com")) &&
		!isYouTubePlaylist(parsedURL) {
		log.Printf("Identified %s as YouTube URL", targetURL)
		if d.youtubeExtractor != nil {
			result, err := d.youtubeExtractor.Extract(targetURL)
			if err != nil {
				return result, fmt.Errorf("youtube extraction failed: %w", err)
			}
			return result, nil
		}
		result, err := d.unimplementedOrFailedInitExtractor("youtube", targetURL, d.youtubeExtractor == nil)
		return result, err
	}

	// 2. Check for Reddit
	if strings.Contains(hostname, "reddit.com") || strings.Contains(hostname, "redd.it") {
		log.Printf("Identified %s as Reddit URL", targetURL)
		if d.redditExtractor != nil {
			result, err := d.redditExtractor.Extract(targetURL)
			if err != nil {
				return result, fmt.Errorf("reddit extraction failed: %w", err)
			}
			return result, nil
		}
		result, err := d.unimplementedOrFailedInitExtractor("reddit", targetURL, d.redditExtractor == nil)
		return result, err
	}

	// 3. Check for Twitter/X (only for /extract endpoint)
	if isTwitterDomain(hostname) {
		log.Printf("Identified %s as Twitter/X URL", targetURL)

		// Only process Twitter URLs via /extract endpoint
		if endpoint != "/extract" {
			log.Printf("Twitter extraction skipped - only available via /extract endpoint")
			result := &ExtractedResult{
				URL:                   targetURL,
				SourceType:            "twitter",
				ProcessedSuccessfully: false,
				Error:                 "Twitter extraction is only available via /extract endpoint",
			}
			return result, fmt.Errorf("twitter extraction is only available via /extract endpoint")
		}

		if d.twitterExtractor != nil {
			result, err := d.twitterExtractor.Extract(targetURL)
			if err != nil {
				return result, fmt.Errorf("twitter extraction failed: %w", err)
			}
			return result, nil
		}
		result, err := d.unimplementedOrFailedInitExtractor("twitter", targetURL, d.twitterExtractor == nil)
		return result, err
	}

	// 4. Check for PDF or default to webpage
	resp, isPDF, err := d.CheckContentType(targetURL)
	if err != nil {
		// If content type check fails, fall back to the general webpage extractor.
		log.Printf("Content type check failed for %s: %v. Defaulting to webpage extractor.", targetURL, err)
	} else {
		defer func() {
			err := resp.Body.Close()
			if err != nil {
				log.Printf("Error closing response body: %v", err)
			}
		}()
	}

	if isPDF {
		log.Printf("Dispatching to PDF Extractor for %s", targetURL)
		if d.pdfExtractor != nil {
			// We need to pass the response with the restored body to the extractor
			// This requires a change in the Extractor interface or the PDF extractor implementation
			// For now, we assume the extractor can take a response object.
			// This will be a placeholder for a future change.
			// result, err := d.pdfExtractor.ExtractWithResponse(resp)
			result, err := d.pdfExtractor.Extract(targetURL) // Current implementation
			if err != nil {
				return result, fmt.Errorf("pdf extraction failed: %w", err)
			}
			return result, nil
		}
		result, err := d.unimplementedOrFailedInitExtractor("pdf", targetURL, d.pdfExtractor == nil)
		return result, err
	}

	// 5. Default to General Web Page Extractor
	log.Printf("Identified %s as general webpage URL", targetURL)

	// For the /extract endpoint, use the headless browser if requested.
	// For all other endpoints (like /search), always use the standard extractor.
	if endpoint == "/extract" && useHeadlessBrowser {
		log.Printf("Using JS-enabled (headless) extractor for %s on /extract", targetURL)
		if d.jsWebpageExtractor != nil {
			result, err := d.jsWebpageExtractor.Extract(targetURL)
			if err != nil {
				return result, fmt.Errorf("js webpage extraction failed: %w", err)
			}
			return result, nil
		}
		result, err := d.unimplementedOrFailedInitExtractor("webpage_js", targetURL, d.jsWebpageExtractor == nil)
		return result, err
	}

	// Fallback to the standard webpage extractor for /search or when headless is not requested.
	log.Printf("Using standard webpage extractor for %s (endpoint: %s, useHeadless: %v)", targetURL, endpoint, useHeadlessBrowser)
	if d.webpageExtractor != nil {
		result, err := d.webpageExtractor.Extract(targetURL)
		if err != nil {
			return result, fmt.Errorf("webpage extraction failed: %w", err)
		}
		return result, nil
	}
	result, err := d.unimplementedOrFailedInitExtractor("webpage", targetURL, d.webpageExtractor == nil)
	return result, err
}

func (d *Dispatcher) unimplementedOrFailedInitExtractor(sourceType, targetURL string, initFailed bool) (*ExtractedResult, error) {
	var errMsg string
	if initFailed {
		errMsg = fmt.Sprintf("%s extractor failed to initialize", sourceType)
	} else {
		errMsg = fmt.Sprintf("%s extractor not implemented (this should not happen if init was attempted)", sourceType)
	}
	log.Printf(errMsg + " for URL: " + targetURL)
	return &ExtractedResult{
		URL:                   targetURL,
		SourceType:            sourceType,
		ProcessedSuccessfully: false,
		Error:                 errMsg,
	}, fmt.Errorf("%s", errMsg)
}

// CheckContentType performs a GET request and sniffs the content type.
// It returns true if the content is a PDF, along with the response.
// If it's not a PDF, it returns false and the response, so the body can be reused.
func (d *Dispatcher) CheckContentType(targetURL string) (*http.Response, bool, error) {
	headClient := &http.Client{
		Timeout:   10 * time.Second,
		Transport: d.mainHTTPClient.Transport,
	}
	resp, err := headClient.Get(targetURL)
	if err != nil {
		return nil, false, fmt.Errorf("failed to perform GET request: %w", err)
	}

	// Check Content-Type header first
	contentType := resp.Header.Get("Content-Type")
	if strings.Contains(strings.ToLower(contentType), "application/pdf") {
		log.Printf("Confirmed PDF by Content-Type header: %s", contentType)
		return resp, true, nil
	}

	// Sniff the first 512 bytes of the body
	buffer := make([]byte, 512)
	bytesRead, err := resp.Body.Read(buffer)
	if err != nil && err != io.EOF {
		if err := resp.Body.Close(); err != nil {
			log.Printf("Error closing response body: %v", err)
		}
		return nil, false, fmt.Errorf("failed to read response body for content sniffing: %w", err)
	}

	// Restore the body so it can be read again
	resp.Body = &restorableBody{
		originalBody: resp.Body,
		readPrefix:   buffer[:bytesRead],
	}

	detectedContentType := http.DetectContentType(buffer)
	if strings.Contains(strings.ToLower(detectedContentType), "application/pdf") {
		log.Printf("Confirmed PDF by content sniffing: %s", detectedContentType)
		return resp, true, nil
	}

	log.Printf("Content-Type not PDF. Header: '%s', Sniffed: '%s'", contentType, detectedContentType)
	return resp, false, nil
}

// restorableBody allows the beginning of a response body to be read and then "put back".
type restorableBody struct {
	originalBody io.ReadCloser
	readPrefix   []byte
	prefixRead   bool
}

func (rb *restorableBody) Read(p []byte) (n int, err error) {
	if !rb.prefixRead {
		n = copy(p, rb.readPrefix)
		if n < len(rb.readPrefix) {
			rb.readPrefix = rb.readPrefix[n:]
		} else {
			rb.prefixRead = true
		}
		return n, nil
	}
	return rb.originalBody.Read(p)
}

func (rb *restorableBody) Close() error {
	return rb.originalBody.Close()
}

// isYouTubePlaylist checks if a given URL is a YouTube playlist.
func isYouTubePlaylist(parsedURL *url.URL) bool {
	// A URL is a playlist if the path contains "/playlist"
	if strings.Contains(parsedURL.Path, "/playlist") {
		return true
	}

	// A URL is also a playlist if it has a "list" query parameter
	// but not a "v" (video) parameter.
	queryParams := parsedURL.Query()
	if queryParams.Get("list") != "" && queryParams.Get("v") == "" {
		return true
	}

	return false
}
