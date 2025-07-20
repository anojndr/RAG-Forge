package extractor

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"

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
		youtubeExtractor:   Extractor(ytExtractor), // This can be nil if NewYouTubeExtractor failed
		redditExtractor:    Extractor(rdExtractor),
		twitterExtractor:   Extractor(twExtractor),
		pdfExtractor:       Extractor(pdfExtractor),
		webpageExtractor:   Extractor(wpExtractor),
		jsWebpageExtractor: Extractor(jsWpExtractor),
	}
}

// DispatchAndExtract determines the URL type and calls the appropriate extractor.
func (d *Dispatcher) DispatchAndExtract(targetURL string, maxChars *int) (*ExtractedResult, error) {
	// Default to not using headless browser if context is not provided.
	return d.DispatchAndExtractWithContext(targetURL, "", false, maxChars)
}

// DispatchAndExtractWithContext determines the URL type and calls the appropriate extractor with context.
func (d *Dispatcher) DispatchAndExtractWithContext(targetURL string, endpoint string, useHeadlessBrowser bool, maxChars *int) (*ExtractedResult, error) {
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
			result, err := d.youtubeExtractor.Extract(targetURL, maxChars)
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
			result, err := d.redditExtractor.Extract(targetURL, maxChars)
			if err != nil {
				return result, fmt.Errorf("reddit extraction failed: %w", err)
			}
			return result, nil
		}
		result, err := d.unimplementedOrFailedInitExtractor("reddit", targetURL, d.redditExtractor == nil)
		return result, err
	}

	// 3. Check for Twitter/X
	if IsTwitterDomain(hostname) {
		log.Printf("Identified %s as Twitter/X URL", targetURL)

		if d.twitterExtractor != nil {
			result, err := d.twitterExtractor.Extract(targetURL, maxChars)
			if err != nil {
				return result, fmt.Errorf("twitter extraction failed: %w", err)
			}
			return result, nil
		}
		result, err := d.unimplementedOrFailedInitExtractor("twitter", targetURL, d.twitterExtractor == nil)
		return result, err
	}

	// 4. Optimistic PDF extraction, with fallback to webpage
	if d.pdfExtractor != nil {
		result, err := d.pdfExtractor.Extract(targetURL, maxChars)
		if err != nil {
			// If it's not a PDF, fall back to the webpage extractor.
			if err == ErrNotPDF {
				log.Printf("PDF extraction failed, falling back to webpage extractor for %s", targetURL)
				return d.webpageExtractor.Extract(targetURL, maxChars)
			}
			return result, fmt.Errorf("pdf extraction failed: %w", err)
		}
		return result, nil
	}


	// 5. Default to General Web Page Extractor
	log.Printf("Identified %s as general webpage URL", targetURL)

	// For the /extract endpoint, use the headless browser if requested.
	// For all other endpoints (like /search), always use the standard extractor.
	if endpoint == "/extract" && useHeadlessBrowser {
		log.Printf("Using JS-enabled (headless) extractor for %s on /extract", targetURL)
		if d.jsWebpageExtractor != nil {
			result, err := d.jsWebpageExtractor.Extract(targetURL, maxChars)
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
		result, err := d.webpageExtractor.Extract(targetURL, maxChars)
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
