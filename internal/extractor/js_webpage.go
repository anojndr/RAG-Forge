package extractor

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"

	"web-search-api-for-llms/internal/config"
	"web-search-api-for-llms/internal/logger"
)

// JSWebpageExtractor implements the Extractor interface for JS-heavy web pages.
type JSWebpageExtractor struct {
	BaseExtractor // Embed BaseExtractor for config access
}

// NewJSWebpageExtractor creates a new JSWebpageExtractor.
func NewJSWebpageExtractor(appConfig *config.AppConfig) *JSWebpageExtractor {
	return &JSWebpageExtractor{
		BaseExtractor: BaseExtractor{Config: appConfig},
	}
}

// Extract uses a headless browser to scrape visible text content and title from a webpage.
func (e *JSWebpageExtractor) Extract(url string) (*ExtractedResult, error) {
	log.Printf("JSWebpageExtractor: Starting extraction for URL: %s", url)
	result := &ExtractedResult{
		URL:        url,
		SourceType: "webpage_js",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Launch browser with optimizations
	launcherURL := launcher.New().
		Headless(true).
		Set("--disable-blink-features", "AutomationControlled").
		Set("--no-sandbox").
		Set("--disable-setuid-sandbox").
		Set("--disable-gpu").
		Set("--disable-dev-shm-usage").
		Set("--disable-extensions").
		Set("--disable-plugins").
		Set("--disable-images").
		Set("--disable-javascript-harmony-shipping").
		Set("--disable-background-networking").
		MustLaunch()

	browser := rod.New().ControlURL(launcherURL).MustConnect()
	defer browser.MustClose()

	page := browser.MustPage()
	defer page.MustClose()

	// Set user agent
	userAgent := "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
	page.MustSetUserAgent(&proto.NetworkSetUserAgentOverride{
		UserAgent: userAgent,
	})

	err := page.Context(ctx).Navigate(url)
	if err != nil {
		result.Error = fmt.Sprintf("failed to navigate to page: %v", err)
		logger.LogError("JSWebpageExtractor: Error for %s: %s", url, result.Error)
		return result, err
	}

	page.MustWaitLoad()
	time.Sleep(2 * time.Second) // Wait for JS to render

	// Extract page title
	var title string
	pageInfo, err := page.Info()
	if err != nil {
		log.Printf("JSWebpageExtractor: Could not get page info for %s: %v", url, err)
	} else {
		title = pageInfo.Title
	}

	// Extract text content
	body, err := page.Element("body")
	if err != nil {
		result.Error = fmt.Sprintf("could not get body element: %v", err)
		logger.LogError("JSWebpageExtractor: Error for %s: %s", url, result.Error)
		return result, err
	}

	textContent, err := body.Text()
	if err != nil {
		result.Error = fmt.Sprintf("could not get text content: %v", err)
		logger.LogError("JSWebpageExtractor: Error for %s: %s", url, result.Error)
		return result, err
	}

	result.ProcessedSuccessfully = true
	result.Data = WebpageData{
		TextContent: strings.TrimSpace(textContent),
		Title:       title,
	}

	log.Printf("JSWebpageExtractor: Finished scraping %s. Title: '%s', Text length: %d", url, title, len(textContent))

	return result, nil
}