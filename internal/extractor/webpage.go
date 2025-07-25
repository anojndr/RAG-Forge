package extractor

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/gocolly/colly/v2"

	"web-search-api-for-llms/internal/config"
	"web-search-api-for-llms/internal/logger"
	"web-search-api-for-llms/internal/useragent"
)

// WebpageExtractor implements the Extractor interface for general web pages.
type WebpageExtractor struct {
	BaseExtractor // Embed BaseExtractor for config access
}

// NewWebpageExtractor creates a new WebpageExtractor.
func NewWebpageExtractor(appConfig *config.AppConfig, client *http.Client) *WebpageExtractor {
	return &WebpageExtractor{
		BaseExtractor: NewBaseExtractor(appConfig, client),
	}
}

// Extract uses Colly to scrape visible text content and title from a webpage.
func (e *WebpageExtractor) Extract(url string, endpoint string, maxChars *int, result *ExtractedResult) error {
	slog.Info("WebpageExtractor: Starting extraction", "url", url)
	result.SourceType = "webpage"

	c := colly.NewCollector(
		colly.MaxDepth(1),
		colly.UserAgent(useragent.RandomDesktop()),
	)

	// Create a new http.Client for this request to avoid data races
	// on the shared client. This is a shallow copy, so it will reuse
	// the transport (and thus connection pooling).
	client := *e.HTTPClient
	client.Timeout = 10 * time.Second
	c.SetClient(&client)

	var pageTitle string
	var textContentBuilder strings.Builder
	var collyErr error

	c.OnHTML("title", func(h *colly.HTMLElement) {
		pageTitle = strings.TrimSpace(h.Text)
	})

	// Remove common non-content elements before extracting text
	c.OnHTML("script, style, noscript, iframe, nav, footer, header, aside, form, menu", func(h *colly.HTMLElement) {
		h.DOM.Remove()
	})

	c.OnHTML("body", func(h *colly.HTMLElement) {
		// A more robust way to get clean text content
		textContentBuilder.WriteString(h.DOM.Text())
	})

	c.OnError(func(r *colly.Response, err error) {
		errMsg := fmt.Sprintf("Colly request failed: status_code=%d, error=%v", r.StatusCode, err)
		logger.LogError("WebpageExtractor: Error scraping", "url", url, "error", errMsg)
		collyErr = errors.New(errMsg)
	})

	c.OnScraped(func(r *colly.Response) {
		slog.Info("WebpageExtractor: Finished scraping", "url", url, "title", pageTitle, "text_length", textContentBuilder.Len())
	})

	if err := c.Visit(url); err != nil {
		if collyErr != nil {
			return fmt.Errorf("failed to visit and scrape webpage: %w (colly error: %v)", err, collyErr)
		}
		return fmt.Errorf("failed to visit and scrape webpage: %w", err)
	}

	if collyErr != nil {
		return collyErr
	}

	textContent := strings.TrimSpace(textContentBuilder.String())

	// Truncate if necessary
	if maxChars != nil && len(textContent) > *maxChars {
		textContent = textContent[:*maxChars]
	}

	result.ProcessedSuccessfully = true
	result.Data = WebpageData{
		TextContent: textContent,
		Title:       pageTitle,
	}

	return nil
}

// ExtractFromContent extracts content from a pre-fetched byte slice.
func (e *WebpageExtractor) ExtractFromContent(url string, content []byte, maxChars *int, result *ExtractedResult) error {
	slog.Info("WebpageExtractor: Starting extraction from content", "url", url)
	result.SourceType = "webpage"

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(content)))
	if err != nil {
		return fmt.Errorf("failed to parse content: %w", err)
	}

	pageTitle := strings.TrimSpace(doc.Find("title").Text())
	// Remove common non-content elements before extracting text
	doc.Find("script, style, noscript, iframe, nav, footer, header, aside, form, menu").Remove()
	textContent := strings.TrimSpace(doc.Find("body").Text())

	// Truncate if necessary
	if maxChars != nil && len(textContent) > *maxChars {
		textContent = textContent[:*maxChars]
	}

	result.ProcessedSuccessfully = true
	result.Data = WebpageData{
		TextContent: textContent,
		Title:       pageTitle,
	}

	slog.Info("WebpageExtractor: Finished extracting from content", "url", url, "title", pageTitle, "text_length", len(textContent))
	return nil
}
