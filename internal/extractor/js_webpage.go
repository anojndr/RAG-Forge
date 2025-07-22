package extractor

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"

	"web-search-api-for-llms/internal/browser"
	"web-search-api-for-llms/internal/config"
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
func (e *JSWebpageExtractor) Extract(url string, endpoint string, maxChars *int, result *ExtractedResult) error {
	slog.Info("JSWebpageExtractor: Starting extraction", "url", url)
	result.SourceType = "webpage_js"

	ctx, cancel := context.WithTimeout(context.Background(), e.Config.JSExtractionTimeout)
	defer cancel()

	browserInstance := e.BrowserPool.Get()
	defer e.BrowserPool.Return(browserInstance)

	page, err := browserInstance.Page(proto.TargetCreateTarget{URL: "about:blank"})
	if err != nil {
		return fmt.Errorf("failed to create page: %w", err)
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
		slog.Warn("failed to set user agent", "error", err)
	}

	if err := page.Context(ctx).Navigate(url); err != nil {
		return fmt.Errorf("failed to navigate to %s: %w", url, err)
	}

	// Wait for the network to be mostly idle, but don't fail if it times out
	waitCtx, waitCancel := context.WithTimeout(ctx, 5*time.Second)
	defer waitCancel()
	page.Context(waitCtx).WaitNavigation(proto.PageLifecycleEventNameNetworkAlmostIdle)()


	// Get title
	info, err := page.Context(ctx).Info()
	var title string
	if err != nil {
		slog.Warn("failed to get page info", "url", url, "error", err)
	} else {
		title = info.Title
	}

	// Wait for the body element to be ready
	if _, err := page.Context(ctx).Element("body"); err != nil {
		return fmt.Errorf("failed to find body element on %s: %w", url, err)
	}

	// Try to get content using a robust innerText script
	eval, err := page.Context(ctx).Eval(`() => document.body.innerText`)
	if err != nil {
		return fmt.Errorf("failed to evaluate javascript on %s: %w", url, err)
	}
	textContent := eval.Value.Str()


	slog.Info("JSWebpageExtractor: Finished scraping", "url", url, "title", title, "text_length", len(textContent))

	// Truncate if necessary
	if maxChars != nil && len(textContent) > *maxChars {
		textContent = textContent[:*maxChars]
	}

	result.ProcessedSuccessfully = true
	result.Data = WebpageData{
		TextContent: strings.TrimSpace(textContent),
		Title:       title,
	}

	return nil
}