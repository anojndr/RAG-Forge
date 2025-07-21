package extractor

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"web-search-api-for-llms/internal/browser"
	"web-search-api-for-llms/internal/config"
	"web-search-api-for-llms/internal/logger"
)

// Dispatcher is responsible for identifying the type of URL and calling the appropriate extractor.
type Dispatcher struct {
	Config             *config.AppConfig
	BrowserPool        *browser.Pool
	mainHTTPClient     *http.Client
	extractors         map[string]Extractor
	jsWebpageExtractor Extractor
}

// NewDispatcher creates a new Dispatcher and initializes all concrete extractors.
func NewDispatcher(appConfig *config.AppConfig, browserPool *browser.Pool, client *http.Client) *Dispatcher {
	d := &Dispatcher{
		Config:             appConfig,
		BrowserPool:        browserPool,
		mainHTTPClient:     client,
		extractors:         make(map[string]Extractor),
		jsWebpageExtractor: NewJSWebpageExtractor(appConfig, browserPool, client),
	}

	ytExtractor, err := NewYouTubeExtractor(appConfig, client)
	if err != nil {
		slog.Warn("Failed to initialize YouTubeExtractor. YouTube URLs may not be processed.", "error", err)
	} else {
		d.register("youtube.com", ytExtractor)
		d.register("youtu.be", ytExtractor)
		d.register("youtube-nocookie.com", ytExtractor)
		d.register("music.youtube.com", ytExtractor)
		d.register("gaming.youtube.com", ytExtractor)
		d.register("tv.youtube.com", ytExtractor)
		d.register("m.youtube.com", ytExtractor)
	}

	d.register("reddit.com", NewRedditExtractor(appConfig, client))
	d.register("redd.it", NewRedditExtractor(appConfig, client))
	d.register("twitter.com", NewTwitterExtractor(appConfig, browserPool, client))
	d.register("x.com", NewTwitterExtractor(appConfig, browserPool, client))
	d.register(".pdf", NewPDFExtractor(appConfig, client))
	d.register("webpage", NewWebpageExtractor(appConfig, client))

	return d
}

func (d *Dispatcher) register(domain string, extractor Extractor) {
	if extractor != nil {
		d.extractors[domain] = extractor
	}
}

// DispatchAndExtract determines the URL type and calls the appropriate extractor.
func (d *Dispatcher) DispatchAndExtract(targetURL string, maxChars *int) (*ExtractedResult, error) {
	// Default to not using headless browser if context is not provided.
	return d.DispatchAndExtractWithContext(targetURL, "", maxChars)
}

// DispatchAndExtractWithContext determines the URL type and calls the appropriate extractor with context.
func (d *Dispatcher) DispatchAndExtractWithContext(targetURL string, endpoint string, maxChars *int) (*ExtractedResult, error) {
	slog.Info("Dispatching URL", "url", targetURL, "endpoint", endpoint)

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

	// Check for PDF first since it's a path check
	if strings.HasSuffix(strings.ToLower(parsedURL.Path), ".pdf") {
		if extractor, ok := d.extractors[".pdf"]; ok {
			slog.Debug("Dispatcher found match for PDF", "url", targetURL)
			return extractor.Extract(targetURL, endpoint, maxChars)
		}
	}

	for domain, extractor := range d.extractors {
		if strings.Contains(hostname, domain) {
			slog.Debug("Dispatcher found match", "url", targetURL, "domain", domain)
			return extractor.Extract(targetURL, endpoint, maxChars)
		}
	}

	// For the /extract endpoint, use the headless browser if requested.
	// For all other endpoints (like /search), always use the standard extractor.
	if endpoint == "/extract" {
		slog.Debug("Using JS-enabled (headless) extractor", "url", targetURL, "endpoint", endpoint)
		if d.jsWebpageExtractor != nil {
			result, err := d.jsWebpageExtractor.Extract(targetURL, endpoint, maxChars)
			if err != nil {
				return result, fmt.Errorf("js webpage extraction failed: %w", err)
			}
			return result, nil
		}
		result, err := d.unimplementedOrFailedInitExtractor("webpage_js", targetURL, d.jsWebpageExtractor == nil)
		return result, err
	}

	// Fallback to the standard webpage extractor for /search or when headless is not requested.
	slog.Debug("Using standard webpage extractor", "url", targetURL, "endpoint", endpoint)
	if extractor, ok := d.extractors["webpage"]; ok {
		result, err := extractor.Extract(targetURL, endpoint, maxChars)
		if err != nil {
			return result, fmt.Errorf("webpage extraction failed: %w", err)
		}
		return result, nil
	}
	result, err := d.unimplementedOrFailedInitExtractor("webpage", targetURL, d.extractors["webpage"] == nil)
	return result, err
}

func (d *Dispatcher) unimplementedOrFailedInitExtractor(sourceType, targetURL string, initFailed bool) (*ExtractedResult, error) {
	var errMsg string
	if initFailed {
		errMsg = fmt.Sprintf("%s extractor failed to initialize", sourceType)
	} else {
		errMsg = fmt.Sprintf("%s extractor not implemented (this should not happen if init was attempted)", sourceType)
	}
	slog.Error(errMsg, "url", targetURL)
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
