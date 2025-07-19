package extractor

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"

	"web-search-api-for-llms/internal/browser"
	"web-search-api-for-llms/internal/config"
	"web-search-api-for-llms/internal/logger"
)

// TwitterExtractor implements the Extractor interface for Twitter/X URLs
type TwitterExtractor struct {
	BaseExtractor
	BrowserPool *browser.Pool
}

// NewTwitterExtractor creates a new TwitterExtractor
func NewTwitterExtractor(appConfig *config.AppConfig, browserPool *browser.Pool) *TwitterExtractor {
	return &TwitterExtractor{
		BaseExtractor: BaseExtractor{Config: appConfig},
		BrowserPool:   browserPool,
	}
}

// Extract fetches Twitter/X post content and comments
func (e *TwitterExtractor) Extract(targetURL string) (*ExtractedResult, error) {
	log.Printf("TwitterExtractor: Starting extraction for URL: %s", targetURL)

	result := &ExtractedResult{
		URL:        targetURL,
		SourceType: "twitter",
	}

	// Create a timeout context for the entire extraction
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Extract tweet ID from URL
	tweetID := extractTweetID(targetURL)
	if tweetID == "" {
		result.Error = "could not extract tweet ID from URL"
		logger.LogError("TwitterExtractor: Error for %s: %s", targetURL, result.Error)
		return result, fmt.Errorf(result.Error)
	}

	log.Printf("TwitterExtractor: Extracted Tweet ID: %s for URL: %s", tweetID, targetURL)

	// Check if we have Twitter credentials
	if e.Config.TwitterUsername == "" || e.Config.TwitterPassword == "" {
		result.Error = "Twitter credentials not configured"
		logger.LogError("TwitterExtractor: Missing Twitter credentials for %s", targetURL)
		return result, fmt.Errorf(result.Error)
	}

	// Extract tweet data using browser automation with context
	tweetData, err := e.extractTweetDataWithContext(ctx, tweetID, targetURL)
	if err != nil {
		result.Error = fmt.Sprintf("extraction failed: %v", err)
		logger.LogError("TwitterExtractor: Error extracting data for %s: %v", targetURL, err)
		return result, err
	}

	result.Data = tweetData
	result.ProcessedSuccessfully = true

	log.Printf("TwitterExtractor: Successfully extracted tweet data for %s", targetURL)
	return result, nil
}

// Compile regex once for tweet ID validation
var tweetIDRegex = regexp.MustCompile(`^\d+$`)

// extractTweetID extracts the tweet ID from various Twitter/X URL formats
func extractTweetID(tweetURL string) string {
	// Handle URLs without protocol
	if !strings.Contains(tweetURL, "://") {
		tweetURL = "https://" + tweetURL
	}

	// Parse the URL
	parsedURL, err := url.Parse(tweetURL)
	if err != nil {
		return ""
	}

	// Normalize hostname
	hostname := strings.ToLower(parsedURL.Hostname())

	// Check if it's a Twitter/X domain
	if !isTwitterDomain(hostname) {
		return ""
	}

	// Extract tweet ID from path
	// Common patterns:
	// - https://twitter.com/username/status/1234567890
	// - https://x.com/username/status/1234567890
	// - https://mobile.twitter.com/username/status/1234567890
	pathParts := strings.Split(strings.Trim(parsedURL.Path, "/"), "/")

	for i, part := range pathParts {
		if part == "status" && i+1 < len(pathParts) {
			tweetID := pathParts[i+1]
			// Remove any query parameters or fragments
			if idx := strings.IndexAny(tweetID, "?&#"); idx != -1 {
				tweetID = tweetID[:idx]
			}
			// Validate tweet ID (should be numeric)
			if tweetIDRegex.MatchString(tweetID) {
				return tweetID
			}
		}
	}

	return ""
}

// isTwitterDomain checks if the hostname is a valid Twitter/X domain
func isTwitterDomain(hostname string) bool {
	validDomains := []string{
		"twitter.com",
		"www.twitter.com",
		"mobile.twitter.com",
		"m.twitter.com",
		"x.com",
		"www.x.com",
		"mobile.x.com",
		"m.x.com",
	}

	for _, domain := range validDomains {
		if hostname == domain {
			return true
		}
	}
	return false
}

// extractTweetDataWithContext uses browser automation to extract tweet content and comments with context
func (e *TwitterExtractor) extractTweetDataWithContext(ctx context.Context, tweetID, tweetURL string) (*TwitterData, error) {
	// Check if context is already cancelled
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("context cancelled before extraction: %w", ctx.Err())
	default:
	}

	// Get browser from pool
	browser := e.BrowserPool.Get()
	defer e.BrowserPool.Return(browser)

	page, err := browser.Page(proto.TargetCreateTarget{URL: ""})
	if err != nil {
		return nil, fmt.Errorf("failed to create page: %w", err)
	}
	defer page.MustClose()

	// Set user agent using the correct Rod API
	userAgent := "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
	page.MustSetUserAgent(&proto.NetworkSetUserAgentOverride{
		UserAgent: userAgent,
	})

	// Try to load saved cookies
	cookiesFile := "twitter_cookies.json"
	if e.loadCookies(page, cookiesFile) {
		log.Printf("TwitterExtractor: Loaded saved session cookies")
		// Test if we're still logged in by navigating to the home page with a timeout
		log.Printf("TwitterExtractor: Navigating to x.com/home to check session status")
		err := page.Timeout(10 * time.Second).Navigate("https://x.com/home")
		if err != nil {
			log.Printf("TwitterExtractor: Failed to navigate to home page to check session (%v), assuming session is expired and logging in.", err)
			if loginErr := e.loginToTwitter(page); loginErr != nil {
				return nil, fmt.Errorf("login failed: %w", loginErr)
			}
			if saveErr := e.saveCookies(page, cookiesFile); saveErr != nil {
				log.Printf("TwitterExtractor: Failed to save cookies: %v", saveErr)
			}
		} else {
			page.MustWaitLoad()
			time.Sleep(1 * time.Second)

			currentURL := page.MustInfo().URL
			if strings.Contains(currentURL, "/home") {
				log.Printf("TwitterExtractor: Session is still valid, skipping login")
			} else {
				log.Printf("TwitterExtractor: Session expired, logging in")
				if loginErr := e.loginToTwitter(page); loginErr != nil {
					return nil, fmt.Errorf("login failed: %w", loginErr)
				}
				if saveErr := e.saveCookies(page, cookiesFile); saveErr != nil {
					log.Printf("TwitterExtractor: Failed to save cookies: %v", saveErr)
				}
			}
		}
	} else {
		log.Printf("TwitterExtractor: No saved session found, logging in")
		if err := e.loginToTwitter(page); err != nil {
		return nil, fmt.Errorf("login failed: %w", err)
		}
		if err := e.saveCookies(page, cookiesFile); err != nil {
			log.Printf("TwitterExtractor: Failed to save cookies: %v", err)
		}
	}

	// Navigate to the tweet
	log.Printf("TwitterExtractor: Navigating to tweet: %s", tweetURL)
	page.MustNavigate(tweetURL)
	page.MustWaitLoad()
	time.Sleep(2 * time.Second)

	// Extract tweet content and comments
	log.Printf("TwitterExtractor: Extracting tweet content and comments")
	return e.extractTweetAndComments(page)
}

// loginToTwitter handles the login process
func (e *TwitterExtractor) loginToTwitter(page *rod.Page) error {
	// Navigate to Twitter login
	page.MustNavigate("https://x.com/i/flow/login")
	page.MustWaitLoad()
	time.Sleep(1 * time.Second)

	// Enter username
	usernameField := page.MustElement(`input[autocomplete="username"]`)
	usernameField.MustSelectAllText().MustInput(e.Config.TwitterUsername)

	// Click Next button
	log.Printf("TwitterExtractor: Clicking Next button")
	clickResult := page.MustEval(`
		() => {
			const buttons = Array.from(document.querySelectorAll('div[role="button"], button'));
			const nextButton = buttons.find(btn => btn.textContent.trim() === 'Next');
			if (nextButton) {
				nextButton.click();
				return true;
			}
			return false;
		}
	`)

	if !clickResult.Bool() {
		return fmt.Errorf("could not find or click Next button")
	}

	time.Sleep(1 * time.Second)

	// Enter password
	log.Printf("TwitterExtractor: Entering password")
	passwordField := page.MustElement(`input[name="password"]`)
	passwordField.MustSelectAllText().MustInput(e.Config.TwitterPassword)

	// Click Log in button
	log.Printf("TwitterExtractor: Clicking Log in button")
	loginResult := page.MustEval(`
		() => {
			const buttons = Array.from(document.querySelectorAll('div[role="button"], button'));
			const loginButton = buttons.find(btn => btn.textContent.trim() === 'Log in');
			if (loginButton) {
				loginButton.click();
				return true;
			}
			return false;
		}
	`)

	if !loginResult.Bool() {
		return fmt.Errorf("could not find or click Log in button")
	}

	time.Sleep(2 * time.Second)

	// Check if login was successful
	currentURL := page.MustInfo().URL
	log.Printf("TwitterExtractor: Login successful, current URL: %s", currentURL)

	if strings.Contains(currentURL, "/home") || strings.Contains(currentURL, "/i/status") {
		return nil
	}

	if strings.Contains(currentURL, "/login") || strings.Contains(currentURL, "/flow") {
		return fmt.Errorf("login failed - still on login page")
	}

	return nil
}

// extractTweetAndComments extracts the main tweet content and comments
func (e *TwitterExtractor) extractTweetAndComments(page *rod.Page) (*TwitterData, error) {
	var tweetData TwitterData

	// Extract main tweet content
	tweetContentSelectors := []string{
		`[data-testid="tweetText"]`,
		`article [lang]`,
		`[role="article"] [lang]`,
	}

	for _, selector := range tweetContentSelectors {
		elements := page.MustElements(selector)
		for _, element := range elements {
			text := element.MustText()
			if strings.TrimSpace(text) != "" && len(text) > 20 {
				tweetData.TweetContent = text
				break
			}
		}
		if tweetData.TweetContent != "" {
			break
		}
	}

	// Extract tweet author
	authorSelectors := []string{
		`[data-testid="User-Name"] span`,
		`article div[dir="ltr"] span`,
		`[role="article"] span`,
	}

	for _, selector := range authorSelectors {
		elements := page.MustElements(selector)
		for _, element := range elements {
			text := element.MustText()
			if strings.Contains(text, "@") && strings.TrimSpace(text) != "" {
				tweetData.TweetAuthor = text
				break
			}
		}
		if tweetData.TweetAuthor != "" {
			break
		}
	}

	// Extract comments with scrolling
	log.Printf("TwitterExtractor: Scrolling to load comments")

	var allComments []TwitterComment
	maxScrolls := 8   // Back to 8 scrolls
	maxComments := 50 // Back to 50 comments

	for scroll := 0; scroll < maxScrolls && len(allComments) < maxComments; scroll++ {
		articles := page.MustElements(`article`)

		for _, article := range articles {
			comment := e.extractCommentFromArticle(article)
			if comment.Content != "" && comment.Username != "" {
				// Check for duplicates
				isDuplicate := false
				for _, existing := range allComments {
					if existing.Username == comment.Username && existing.Content == comment.Content {
						isDuplicate = true
						break
					}
				}

				if !isDuplicate {
					allComments = append(allComments, comment)
				}
			}
		}

		if scroll%2 == 0 {
			log.Printf("TwitterExtractor: Found %d comments", len(allComments))
		}

		// Scroll to load more comments
		page.MustEval(`() => { window.scrollBy(0, 2000); }`)
		time.Sleep(1200 * time.Millisecond) // Back to 1200ms for better content loading
	}

	tweetData.Comments = allComments
	tweetData.TotalComments = len(allComments)

	return &tweetData, nil
}

// extractCommentFromArticle extracts comment data from an article element
func (e *TwitterExtractor) extractCommentFromArticle(article *rod.Element) TwitterComment {
	var comment TwitterComment

	// Extract username
	usernameElements, err := article.Elements(`[href^="/"]:not([href*="/status/"]):not([href*="/photo/"]):not([href*="/analytics"])`)
	if err == nil {
		for _, elem := range usernameElements {
			href, err := elem.Attribute("href")
			if err != nil {
				continue
			}
			if href != nil && strings.HasPrefix(*href, "/") && !strings.Contains(*href, "/status/") {
				username := strings.TrimPrefix(*href, "/")
				if username != "" && !strings.Contains(username, "/") {
					comment.Username = username
					break
				}
			}
		}
	}

	// Extract author display name
	authorElements, err := article.Elements(`span`)
	if err == nil {
		for _, elem := range authorElements {
			text := elem.MustText()
			if strings.TrimSpace(text) != "" &&
				!strings.Contains(text, "@") &&
				!strings.Contains(text, "·") &&
				!strings.Contains(text, "Reply") &&
				!strings.Contains(text, "Repost") &&
				!strings.Contains(text, "Like") &&
				len(text) < 50 && len(text) > 2 {
				comment.Author = text
				break
			}
		}
	}

	// Extract comment content
	contentElements, err := article.Elements(`[data-testid="tweetText"], [lang]`)
	if err == nil {
		for _, elem := range contentElements {
			text := elem.MustText()
			if strings.TrimSpace(text) != "" && len(text) > 10 {
				comment.Content = text
				break
			}
		}
	}

	// Extract timestamp
	timeElements, err := article.Elements(`time`)
	if err == nil {
		for _, elem := range timeElements {
			timestamp := elem.MustText()
			if strings.TrimSpace(timestamp) != "" {
				comment.Timestamp = timestamp
				break
			}
		}
	}

	// Extract engagement stats
	statElements, err := article.Elements(`[role="group"] span`)
	if err == nil {
		for _, elem := range statElements {
			text := elem.MustText()
			text = strings.TrimSpace(text)
			if text != "" && (strings.Contains(text, "K") ||
				(len(text) <= 4 && strings.ContainsAny(text, "0123456789"))) {
				if comment.Replies == "" {
					comment.Replies = text
				} else if comment.Retweets == "" {
					comment.Retweets = text
				} else if comment.Likes == "" {
					comment.Likes = text
					break
				}
			}
		}
	}

	return comment
}

// saveCookies saves browser cookies to a file
func (e *TwitterExtractor) saveCookies(page *rod.Page, filename string) error {
	cookies := page.MustCookies()

	var cookieData []map[string]interface{}
	for _, cookie := range cookies {
		cookieMap := map[string]interface{}{
			"name":     cookie.Name,
			"value":    cookie.Value,
			"domain":   cookie.Domain,
			"path":     cookie.Path,
			"expires":  cookie.Expires,
			"httpOnly": cookie.HTTPOnly,
			"secure":   cookie.Secure,
			"sameSite": cookie.SameSite,
		}
		cookieData = append(cookieData, cookieMap)
	}

	jsonData, err := json.MarshalIndent(cookieData, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal cookies: %w", err)
	}

	err = os.WriteFile(filename, jsonData, 0644)
	if err != nil {
		return fmt.Errorf("failed to save cookies: %w", err)
	}

	log.Printf("TwitterExtractor: Session cookies saved to %s", filename)
	return nil
}

// loadCookies loads browser cookies from a file
func (e *TwitterExtractor) loadCookies(page *rod.Page, filename string) bool {
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		return false
	}

	data, err := os.ReadFile(filename)
	if err != nil {
		log.Printf("TwitterExtractor: Could not read cookies file: %v", err)
		return false
	}

	var cookieData []map[string]interface{}
	err = json.Unmarshal(data, &cookieData)
	if err != nil {
		log.Printf("TwitterExtractor: Could not parse cookies: %v", err)
		return false
	}

	for _, cookieMap := range cookieData {
		cookie := &proto.NetworkCookieParam{}

		if name, ok := cookieMap["name"].(string); ok {
			cookie.Name = name
		}
		if value, ok := cookieMap["value"].(string); ok {
			cookie.Value = value
		}
		if domain, ok := cookieMap["domain"].(string); ok {
			cookie.Domain = domain
		}
		if path, ok := cookieMap["path"].(string); ok {
			cookie.Path = path
		}
		if httpOnly, ok := cookieMap["httpOnly"].(bool); ok {
			cookie.HTTPOnly = httpOnly
		}
		if secure, ok := cookieMap["secure"].(bool); ok {
			cookie.Secure = secure
		}

		page.MustSetCookies(cookie)
	}

	return true
}
