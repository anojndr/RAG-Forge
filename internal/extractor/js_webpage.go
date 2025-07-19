package extractor

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/go-rod/rod"
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
func NewJSWebpageExtractor(appConfig *config.AppConfig, browserPool *browser.Pool) *JSWebpageExtractor {
	return &JSWebpageExtractor{
		BaseExtractor: BaseExtractor{Config: appConfig},
		BrowserPool:   browserPool,
	}
}

// Extract uses a headless browser (chromedp) to get the visible text from a URL.
func (e *JSWebpageExtractor) Extract(url string) (*ExtractedResult, error) {
	log.Printf("JSWebpageExtractor: Starting extraction for URL: %s", url)
	result := &ExtractedResult{
		URL:        url,
		SourceType: "webpage_js",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	// Get browser from pool
	browser := e.BrowserPool.Get()
	defer e.BrowserPool.Return(browser)

	page, err := browser.Page(proto.TargetCreateTarget{URL: ""})
	if err != nil {
		return nil, fmt.Errorf("failed to create page: %w", err)
	}
	defer page.MustClose()

	// Set user agent
	userAgent := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/119.0.0.0 Safari/537.36"
	page.MustSetUserAgent(&proto.NetworkSetUserAgentOverride{
		UserAgent: userAgent,
	})

	var title, textContent string

	err = rod.Try(func() {
		page.Context(ctx).MustNavigate(url)
		page.Context(ctx).MustWaitLoad()

		// Get title
		title = page.Context(ctx).MustInfo().Title

		// Extract text from the body, trying to get only visible text
		textContentEval := page.Context(ctx).MustEval(`
			() => {
				// Remove script and style tags
				document.querySelectorAll('script, style, noscript, iframe, svg, footer, header, nav').forEach(el => el.remove());
				// Get the text content of the body
				return document.body.innerText;
			}
		`)
		textContent = textContentEval.Str()
	})

	if err != nil {
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		errMsg := fmt.Sprintf("rod execution failed: %v", err)
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