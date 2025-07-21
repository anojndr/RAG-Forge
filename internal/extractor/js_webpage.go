package extractor

import (
	"context"
	"fmt"
	"net/http"
	"log/slog"
	"strings"

	"github.com/go-rod/rod/lib/proto"
	"github.com/go-rod/rod"

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
	slog.Info("JSWebpageExtractor: Starting extraction", "url", url)
	result := &ExtractedResult{
		URL:        url,
		SourceType: "webpage_js",
	}

	ctx, cancel := context.WithTimeout(context.Background(), e.Config.JSExtractionTimeout)
	defer cancel()

	browser := e.BrowserPool.Get()
	defer e.BrowserPool.Return(browser)

	page, err := browser.Page(proto.TargetCreateTarget{URL: "about:blank"})
	if err != nil {
		result.Error = fmt.Sprintf("failed to create page: %v", err)
		logger.LogError(result.Error)
		return result, err
	}
	defer page.MustClose()

	// Intercept and block non-essential requests
	router := page.HijackRequests()
	defer router.Stop()

	router.MustAdd("*", func(ctx *rod.Hijack) {
		// Allow only document and data-fetching requests
		switch ctx.Request.Type() {
		case proto.NetworkResourceTypeDocument,
			proto.NetworkResourceTypeXHR,
			proto.NetworkResourceTypeFetch:
			ctx.ContinueRequest(&proto.FetchContinueRequest{})
		default:
			// Block everything else: images, css, fonts, media, etc.
			ctx.Response.Fail(proto.NetworkErrorReasonBlockedByClient)
		}
	})
	go router.Run()

	// Set user agent
	userAgent := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"
	if err := page.SetUserAgent(&proto.NetworkSetUserAgentOverride{UserAgent: userAgent}); err != nil {
		logger.LogError("failed to set user agent: %v", err)
	}

	var title, textContent string

	// Event handler for redirects
	go page.EachEvent(func(e *proto.PageFrameNavigated) {
		slog.Debug("JSWebpageExtractor: Page navigated (redirect)", "url", e.Frame.URL)
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

	// Wait for the body element to be ready
	page.Context(ctx).MustElement("body")

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
		slog.Debug("JSWebpageExtractor: No text content from selection, falling back to innerText", "url", url)
		eval, err := page.Context(ctx).Eval(`() => document.body.innerText`)
		if err != nil {
			errMsg := fmt.Sprintf("failed to evaluate javascript on %s: %v", url, err)
			result.Error = errMsg
			logger.LogError(errMsg)
			return result, err
		}
		textContent = eval.Value.Str()
	}

	slog.Info("JSWebpageExtractor: Finished scraping", "url", url, "title", title, "text_length", len(textContent))

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