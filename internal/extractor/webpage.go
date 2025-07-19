package extractor

import (
	"fmt"
	"log"
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
func (e *WebpageExtractor) Extract(url string, maxChars *int) (*ExtractedResult, error) {
	log.Printf("WebpageExtractor: Starting extraction for URL: %s", url)
	result := &ExtractedResult{
		URL:        url,
		SourceType: "webpage",
	}

	c := colly.NewCollector(
		colly.MaxDepth(1),
		colly.UserAgent(useragent.RandomDesktop()),
	)

	c.SetRequestTimeout(10 * time.Second)

	var pageTitle string
	var textContentBuilder strings.Builder

	c.OnHTML("title", func(h *colly.HTMLElement) {
		pageTitle = strings.TrimSpace(h.Text)
	})

	c.OnHTML("script, style, noscript, iframe, nav, footer, header", func(h *colly.HTMLElement) {
		h.DOM.Remove()
	})

	c.OnHTML("body", func(h *colly.HTMLElement) {
		textContentBuilder.WriteString(h.Text)
	})

	c.OnError(func(r *colly.Response, err error) {
		errMsg := fmt.Sprintf("Colly request failed: status_code=%d, error=%v", r.StatusCode, err)
		result.Error = errMsg
		logger.LogError("WebpageExtractor: Error scraping %s: %s", url, errMsg)
	})

	c.OnScraped(func(r *colly.Response) {
		log.Printf("WebpageExtractor: Finished scraping %s. Title: '%s', Text length: %d", url, pageTitle, textContentBuilder.Len())
	})

	err := c.Visit(url)
	if err != nil {
		if result.Error == "" {
			result.Error = fmt.Sprintf("failed to visit and scrape webpage: %v", err)
		}
		logger.LogError("WebpageExtractor: Visit error for %s: %v", url, err)
		return result, err
	}

	if result.Error != "" {
		return result, fmt.Errorf(result.Error)
	}

	result.ProcessedSuccessfully = true
	result.Data = WebpageData{
		TextContent: strings.TrimSpace(textContentBuilder.String()),
		Title:       pageTitle,
	}

	if maxChars != nil {
		if data, ok := result.Data.(WebpageData); ok {
			if len(data.TextContent) > *maxChars {
				data.TextContent = data.TextContent[:*maxChars]
				result.Data = data
			}
		}
	}

	return result, nil
}

// ExtractFromContent extracts content from a pre-fetched byte slice.
func (e *WebpageExtractor) ExtractFromContent(url string, content []byte) (*ExtractedResult, error) {
	log.Printf("WebpageExtractor: Starting extraction from content for URL: %s", url)
	result := &ExtractedResult{
		URL:        url,
		SourceType: "webpage",
	}

	var pageTitle string
	var textContentBuilder strings.Builder

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(content)))
	if err != nil {
		return nil, fmt.Errorf("failed to parse content: %w", err)
	}

	pageTitle = strings.TrimSpace(doc.Find("title").Text())
	doc.Find("script, style, noscript, iframe, nav, footer, header").Remove()
	textContentBuilder.WriteString(doc.Find("body").Text())

	result.ProcessedSuccessfully = true
	result.Data = WebpageData{
		TextContent: strings.TrimSpace(textContentBuilder.String()),
		Title:       pageTitle,
	}

	log.Printf("WebpageExtractor: Finished extracting from content for %s. Title: '%s', Text length: %d", url, pageTitle, textContentBuilder.Len())
	return result, nil
}
