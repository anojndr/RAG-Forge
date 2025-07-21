package extractor

import (
	"context"
	"fmt"
	"net/http"
	"log"
	"strings"
	"time"

	"github.com/go-rod/rod/lib/proto"

	"web-search-api-for-llms/internal/browser"
	"web-search-api-for-llms/internal/config"
	"web-search-api-for-llms/internal/logger"
)

// JSWebpageExtractor implements the Extractor interface for general web pages that require JavaScript rendering.
type JSWebpageExtractor struct {
	BaseExtractor // Embed BaseExtractor for config access
	BrowserPool   *browser.Pool
}

// NewJSWebpageExtractor creates a new JSWebpageExtractor.
func NewJSWebpageExtractor(appConfig *config.AppConfig, browserPool *browser.Pool, client *http.Client) *JSWebpageExtractor {
	return &JSWebpageExtractor{
		BaseExtractor: NewBaseExtractor(appConfig, client),
		BrowserPool:   browserPool,
	}
}

// Extract uses a headless browser (chromedp) to get the visible text from a URL.
func (e *JSWebpageExtractor) Extract(url string, endpoint string, maxChars *int) (*ExtractedResult, error) {
	log.Printf("JSWebpageExtractor: Starting extraction for URL: %s", url)
	result := &ExtractedResult{
		URL:        url,
		SourceType: "webpage_js",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	browser := e.BrowserPool.Get()
	defer e.BrowserPool.Return(browser)

	page, err := browser.Page(proto.TargetCreateTarget{URL: ""})
	if err != nil {
		result.Error = fmt.Sprintf("failed to create page: %v", err)
		logger.LogError(result.Error)
		return result, err
	}
	defer page.MustClose()

	// Set user agent
	userAgent := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"
	if err := page.SetUserAgent(&proto.NetworkSetUserAgentOverride{UserAgent: userAgent}); err != nil {
		logger.LogError("failed to set user agent: %v", err)
	}

	var title, textContent string

	// Event handler for redirects
	go page.EachEvent(func(e *proto.PageFrameNavigated) {
		log.Printf("JSWebpageExtractor: Page navigated to %s (redirect)", e.Frame.URL)
	})()

	if err := page.Context(ctx).Navigate(url); err != nil {
		errMsg := fmt.Sprintf("failed to navigate to %s: %v", url, err)
		result.Error = errMsg
		logger.LogError(errMsg)
		return result, err
	}

	page.Context(ctx).WaitNavigation(proto.PageLifecycleEventNameNetworkAlmostIdle)()
	if ctx.Err() != nil {
		logger.LogError("failed to wait for network idle for %s: %v", url, ctx.Err())
		// Continue execution, as this is not always a fatal error
	}

	// Get title
	info, err := page.Context(ctx).Info()
	if err != nil {
		logger.LogError("failed to get page info for %s: %v", url, err)
	} else {
		title = info.Title
	}

	// Scroll to the bottom of the page to trigger lazy loading, waiting for stability
	if err := page.Context(ctx).WaitStable(2 * time.Second); err != nil {
		logger.LogError("JSWebpageExtractor: Error waiting for page to be stable after scrolling for %s: %v", url, err)
	}

	// Select all text using JavaScript
	_, err = page.Context(ctx).Eval(`() => {
		const range = document.createRange();
		range.selectNode(document.body);
		window.getSelection().removeAllRanges();
		window.getSelection().addRange(range);
	}`)
	if err != nil {
		errMsg := fmt.Sprintf("failed to select all text with javascript on %s: %v", url, err)
		result.Error = errMsg
		logger.LogError(errMsg)
		return result, err
	}

	// Get selected text
	text, err := page.Context(ctx).Eval("() => window.getSelection().toString()")
	if err != nil {
		errMsg := fmt.Sprintf("failed to get selected text on %s: %v", url, err)
		result.Error = errMsg
		logger.LogError(errMsg)
		return result, err
	}
	textContent = text.Value.Str()

	if len(strings.TrimSpace(textContent)) == 0 {
		log.Printf("JSWebpageExtractor: No text content found after selection for %s. Falling back to innerText.", url)
		eval, err := page.Context(ctx).Eval(`() => document.body.innerText`)
		if err != nil {
			errMsg := fmt.Sprintf("failed to evaluate javascript on %s: %v", url, err)
			result.Error = errMsg
			logger.LogError(errMsg)
			return result, err
		}
		textContent = eval.Value.Str()
	}

	log.Printf("JSWebpageExtractor: Finished scraping %s. Title: '%s', Text length: %d", url, title, len(textContent))

	result.ProcessedSuccessfully = true
	result.Data = WebpageData{
		TextContent: strings.TrimSpace(textContent),
		Title:       title,
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