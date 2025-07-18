package extractor

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/chromedp/chromedp"

	"web-search-api-for-llms/internal/config"
	"web-search-api-for-llms/internal/logger"
)

// JSWebpageExtractor implements the Extractor interface for general web pages that require JavaScript rendering.
type JSWebpageExtractor struct {
	BaseExtractor // Embed BaseExtractor for config access
}

// NewJSWebpageExtractor creates a new JSWebpageExtractor.
func NewJSWebpageExtractor(appConfig *config.AppConfig) *JSWebpageExtractor {
	return &JSWebpageExtractor{
		BaseExtractor: BaseExtractor{Config: appConfig},
	}
}

// Extract uses a headless browser (chromedp) to get the visible text from a URL.
func (e *JSWebpageExtractor) Extract(url string) (*ExtractedResult, error) {
	log.Printf("JSWebpageExtractor: Starting extraction for URL: %s", url)
	result := &ExtractedResult{
		URL:        url,
		SourceType: "webpage_js",
	}

	// Create a new context
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.UserAgent("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/119.0.0.0 Safari/537.36"),
	)

	allocCtx, cancel := chromedp.NewExecAllocator(context.Background(), opts...)
	defer cancel()

	ctx, cancel := chromedp.NewContext(allocCtx, chromedp.WithLogf(log.Printf))
	defer cancel()

	// Create a timeout
	ctx, cancel = context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	var title, textContent string

	err := chromedp.Run(ctx,
		chromedp.Navigate(url),
		chromedp.WaitVisible("body", chromedp.ByQuery),
		chromedp.Title(&title),
		// Extract text from the body, trying to get only visible text
		chromedp.Evaluate(`
			(function() {
				// Remove script and style tags
				document.querySelectorAll('script, style, noscript, iframe, svg, footer, header, nav').forEach(el => el.remove());
				// Get the text content of the body
				return document.body.innerText;
			})();
		`, &textContent),
	)

	if err != nil {
		errMsg := fmt.Sprintf("chromedp execution failed: %v", err)
		result.Error = errMsg
		logger.LogError("JSWebpageExtractor: Error extracting %s: %s", url, errMsg)
		return result, err
	}

	log.Printf("JSWebpageExtractor: Finished scraping %s. Title: '%s', Text length: %d", url, title, len(textContent))

	result.ProcessedSuccessfully = true
	result.Data = WebpageData{
		TextContent: strings.TrimSpace(textContent),
		Title:       title,
	}

	return result, nil
}