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
// DispatchAndExtract is deprecated. Use DispatchAndExtractWithContext with a pooled ExtractedResult.
// This function is kept for simple, non-pooled, single-URL extractions if ever needed,
// but the primary flow should use the pooled version.
func (d *Dispatcher) DispatchAndExtract(targetURL string, maxChars *int) (*ExtractedResult, error) {
	result := ExtractedResultPool.Get().(*ExtractedResult)
	result.Reset()
	result.URL = targetURL

	err := d.DispatchAndExtractWithContext(targetURL, "", maxChars, result)
	if err != nil {
		result.ProcessedSuccessfully = false
		result.Error = err.Error()
	}
	return result, err
}

// DispatchAndExtractWithContext determines the URL type and calls the appropriate extractor with context.
func (d *Dispatcher) DispatchAndExtractWithContext(targetURL string, endpoint string, maxChars *int, result *ExtractedResult) error {
	slog.Info("Dispatching URL", "url", targetURL, "endpoint", endpoint)

	parsedURL, err := url.Parse(targetURL)
	if err != nil {
		wrappedErr := fmt.Errorf("failed to parse URL %s: %w", targetURL, err)
		logger.LogError("Error: %v", wrappedErr)
		result.Error = "Invalid URL format"
		result.SourceType = "unknown"
		return wrappedErr
	}

	hostname := strings.ToLower(parsedURL.Hostname())

	// Check for PDF first since it's a path check
	if strings.HasSuffix(strings.ToLower(parsedURL.Path), ".pdf") {
		if extractor, ok := d.extractors[".pdf"]; ok {
			slog.Debug("Dispatcher found match for PDF", "url", targetURL)
			return extractor.Extract(targetURL, endpoint, maxChars, result)
		}
	}

	for domain, extractor := range d.extractors {
		if strings.Contains(hostname, domain) {
			slog.Debug("Dispatcher found match", "url", targetURL, "domain", domain)
			return extractor.Extract(targetURL, endpoint, maxChars, result)
		}
	}

	// For the /extract endpoint, use the headless browser if requested.
	// For all other endpoints (like /search), always use the standard extractor.
	if endpoint == "/extract" {
		slog.Debug("Using JS-enabled (headless) extractor", "url", targetURL, "endpoint", endpoint)
		if d.jsWebpageExtractor != nil {
			err := d.jsWebpageExtractor.Extract(targetURL, endpoint, maxChars, result)
			if err != nil {
				return fmt.Errorf("js webpage extraction failed: %w", err)
			}
			return nil
		}
		return d.unimplementedOrFailedInitExtractor("webpage_js", result, d.jsWebpageExtractor == nil)
	}

	// Fallback to the standard webpage extractor for /search or when headless is not requested.
	slog.Debug("Using standard webpage extractor", "url", targetURL, "endpoint", endpoint)
	if extractor, ok := d.extractors["webpage"]; ok {
		err := extractor.Extract(targetURL, endpoint, maxChars, result)
		if err != nil {
			return fmt.Errorf("webpage extraction failed: %w", err)
		}
		return nil
	}
	return d.unimplementedOrFailedInitExtractor("webpage", result, d.extractors["webpage"] == nil)
}

func (d *Dispatcher) unimplementedOrFailedInitExtractor(sourceType string, result *ExtractedResult, initFailed bool) error {
	var errMsg string
	if initFailed {
		errMsg = fmt.Sprintf("%s extractor failed to initialize", sourceType)
	} else {
		errMsg = fmt.Sprintf("%s extractor not implemented (this should not happen if init was attempted)", sourceType)
	}
	slog.Error(errMsg, "url", result.URL)
	result.SourceType = sourceType
	result.Error = errMsg
	return fmt.Errorf("%s", errMsg)
}
