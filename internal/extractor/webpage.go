package extractor

import (
	"fmt"
	"log"
	"strings"
	"time"

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
func NewWebpageExtractor(appConfig *config.AppConfig) *WebpageExtractor {
	return &WebpageExtractor{
		BaseExtractor: BaseExtractor{Config: appConfig},
	}
}

// Extract uses Colly to scrape visible text content and title from a webpage.
func (e *WebpageExtractor) Extract(url string) (*ExtractedResult, error) {
	log.Printf("WebpageExtractor: Starting extraction for URL: %s", url)
	result := &ExtractedResult{
		URL:        url,
		SourceType: "webpage",
	}

	c := colly.NewCollector(
		// MaxDepth is 1, so we only scrap the given page
		colly.MaxDepth(1),
		// Randomized User-Agent to mimic different browsers
		colly.UserAgent(useragent.RandomDesktop()),
		// Async for potentially faster scraping, though for single URL it's less critical
		// colly.Async(true), // Let's keep it synchronous for simplicity within a single extractor call
	)

	// Set a timeout for the request
	c.SetRequestTimeout(5 * time.Second)

	var pageTitle string
	var textContentBuilder strings.Builder

	// Extract page title
	c.OnHTML("title", func(h *colly.HTMLElement) {
		pageTitle = strings.TrimSpace(h.Text)
	})

	// Remove script and style elements to avoid extracting their content
	c.OnHTML("script, style", func(h *colly.HTMLElement) {
		h.DOM.Remove()
	})

	// Extract all visible text from the body
	c.OnHTML("body", func(h *colly.HTMLElement) {
		textContentBuilder.WriteString(h.Text)
	})

	c.OnError(func(r *colly.Response, err error) {
		errMsg := fmt.Sprintf("Colly request failed: status_code=%d, error=%v", r.StatusCode, err)
		result.Error = errMsg
		logger.LogError("WebpageExtractor: Error scraping %s: %s", url, errMsg)
		// Note: 'err' here will be passed back by c.Visit, so we just populate result.Error
	})

	c.OnScraped(func(r *colly.Response) {
		log.Printf("WebpageExtractor: Finished scraping %s. Title: '%s', Text length: %d", url, pageTitle, textContentBuilder.Len())
	})

	err := c.Visit(url)
	if err != nil {
		// If result.Error wasn't set by OnError, set it now.
		if result.Error == "" {
			result.Error = fmt.Sprintf("failed to visit and scrape webpage: %v", err)
		}
		logger.LogError("WebpageExtractor: Visit error for %s: %v", url, err)
		return result, err // Return the error from c.Visit
	}

	// If OnError was triggered, result.Error would be set.
	// If c.Visit succeeded but OnError also set an error, we prioritize that.
	if result.Error != "" {
		return result, fmt.Errorf(result.Error) // Ensure an error is returned if result.Error is populated
	}

	result.ProcessedSuccessfully = true
	result.Data = WebpageData{
		TextContent: strings.TrimSpace(textContentBuilder.String()),
		Title:       pageTitle,
	}

	return result, nil
}
