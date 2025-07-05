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

	// Extract readable text content using a more sophisticated approach
	// First, remove non-readable elements entirely
	c.OnHTML("script, style, noscript, nav, footer, header, aside, .sidebar, .nav, .menu, .advertisement, .ads, .social-media, .share-buttons", func(h *colly.HTMLElement) {
		h.DOM.Remove()
	})

	// Focus on main content areas and readable text elements
	c.OnHTML("main, article, .content, .post-content, .entry-content, .main-content, div[role='main'], .article-body, .story-body", func(h *colly.HTMLElement) {
		// Extract text from semantic elements within main content
		h.ForEach("p, h1, h2, h3, h4, h5, h6, blockquote, pre, code, li", func(i int, elem *colly.HTMLElement) {
			text := strings.TrimSpace(elem.Text)
			if text != "" && len(text) > 10 { // Filter out very short text that might be noise
				textContentBuilder.WriteString(text)
				textContentBuilder.WriteString("\n\n")
			}
		})
	})

	// Fallback: if no main content areas found, extract from common readable elements
	// but with more stringent filtering
	c.OnHTML("body", func(h *colly.HTMLElement) {
		// Only proceed if we haven't extracted much content yet
		if textContentBuilder.Len() < 200 {
			h.ForEach("p, h1, h2, h3, h4, h5, h6, article p, .text, .description", func(i int, elem *colly.HTMLElement) {
				// Skip elements that are likely navigation or metadata
				if elem.Attr("class") != "" {
					class := strings.ToLower(elem.Attr("class"))
					if strings.Contains(class, "nav") || strings.Contains(class, "menu") ||
						strings.Contains(class, "footer") || strings.Contains(class, "header") ||
						strings.Contains(class, "sidebar") || strings.Contains(class, "ad") {
						return
					}
				}

				text := strings.TrimSpace(elem.Text)
				if text != "" && len(text) > 20 { // Higher threshold for fallback extraction
					textContentBuilder.WriteString(text)
					textContentBuilder.WriteString("\n\n")
				}
			})
		}
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
