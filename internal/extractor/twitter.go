package extractor

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
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

// TweetDetailResponse defines the structure for the entire JSON response from the Twitter API.
type TweetDetailResponse struct {
	Data struct {
		ThreadedConversationWithInjectionsV2 struct {
			Instructions []struct {
				Type    string  `json:"type"`
				Entries []Entry `json:"entries"`
			} `json:"instructions"`
		} `json:"threaded_conversation_with_injections_v2"`
	} `json:"data"`
}

// Entry represents a single entry in the timeline, which could be a tweet, a comment, or a cursor.
type Entry struct {
	EntryID string `json:"entryId"`
	Content struct {
		EntryType   string `json:"entryType"`
		Value       string `json:"value"` // For cursors
		ItemContent struct {
			ItemType     string `json:"itemType"`
			TweetResults struct {
				Result TweetResult `json:"result"`
			} `json:"tweet_results"`
		} `json:"itemContent"`
		Items []struct {
			Item struct {
				ItemContent struct {
					TweetResults struct {
						Result TweetResult `json:"result"`
					} `json:"tweet_results"`
				} `json:"itemContent"`
			} `json:"item"`
		} `json:"items"`
	} `json:"content"`
}

// TweetResult contains the main data of a tweet.
type TweetResult struct {
	Typename string `json:"__typename"`
	RestID   string `json:"rest_id"`
	Core     struct {
		UserResults struct {
			Result UserResult `json:"result"`
		} `json:"user_results"`
	} `json:"core"`
	Legacy TweetLegacy `json:"legacy"`
}

// UserResult holds information about a Twitter user.
type UserResult struct {
	Typename string `json:"__typename"`
	Legacy   struct {
		Name       string `json:"name"`
		ScreenName string `json:"screen_name"`
	} `json:"legacy"`
}

// TweetLegacy contains the textual content and metadata of a tweet.
type TweetLegacy struct {
	FullText      string `json:"full_text"`
	CreatedAt     string `json:"created_at"`
	FavoriteCount int    `json:"favorite_count"`
	ReplyCount    int    `json:"reply_count"`
	RetweetCount  int    `json:"retweet_count"`
}

// TwitterExtractor implements the Extractor interface for Twitter/X URLs
type TwitterExtractor struct {
	BaseExtractor
	BrowserPool *browser.Pool
	Config      *config.AppConfig
}

// NewTwitterExtractor creates a new TwitterExtractor
func NewTwitterExtractor(appConfig *config.AppConfig, browserPool *browser.Pool, client *http.Client) *TwitterExtractor {
	return &TwitterExtractor{
		BaseExtractor: NewBaseExtractor(appConfig, client),
		BrowserPool:   browserPool,
		Config:        appConfig,
	}
}

// Extract fetches Twitter/X post content and comments

func (e *TwitterExtractor) Extract(targetURL string, maxChars *int) (*ExtractedResult, error) {
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

	if maxChars != nil {
		if data, ok := result.Data.(*TwitterData); ok {
			if len(data.TweetContent) > *maxChars {
				data.TweetContent = data.TweetContent[:*maxChars]
				result.Data = data
			}
		}
	}

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
	if !IsTwitterDomain(hostname) {
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
func IsTwitterDomain(hostname string) bool {
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
			page.MustWaitNavigation()

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

	// Use a channel to receive the API response
	apiResponseChan := make(chan *TweetDetailResponse)
	errChan := make(chan error, 1)

	// Set up request hijacking
	router := page.HijackRequests()
	defer func() {
		if err := router.Stop(); err != nil {
			logger.LogError("TwitterExtractor: Error stopping router: %v", err)
		}
	}()

	router.MustAdd("*TweetDetail*", func(ctx *rod.Hijack) {
		err := ctx.LoadResponse(http.DefaultClient, true)
		if err != nil {
			errChan <- fmt.Errorf("could not load response: %w", err)
			return
		}

		var apiResponse TweetDetailResponse
		if err := json.Unmarshal(ctx.Response.Payload().Body, &apiResponse); err != nil {
			errChan <- fmt.Errorf("error parsing TweetDetail JSON: %v", err)
			return
		}
		apiResponseChan <- &apiResponse
	})

	go router.Run()

	// Navigate to the tweet
	log.Printf("TwitterExtractor: Navigating to tweet: %s", tweetURL)
	if err := page.Navigate(tweetURL); err != nil {
		return nil, fmt.Errorf("failed to navigate to tweet: %w", err)
	}

	// Wait for the API response or timeout
	select {
	case apiResponse := <-apiResponseChan:
		log.Printf("TwitterExtractor: Successfully captured TweetDetail API response")
		return e.parseTweetDetailResponse(apiResponse)
	case err := <-errChan:
		return nil, err
	case <-time.After(30 * time.Second):
		return nil, fmt.Errorf("timed out waiting for TweetDetail API response")
	}
}

// loginToTwitter handles the login process
func (e *TwitterExtractor) loginToTwitter(page *rod.Page) error {
	// Navigate to Twitter login
	page.MustNavigate("https://x.com/i/flow/login")
	page.MustElement(`input[autocomplete="username"]`).MustWaitVisible()

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

	page.MustElement(`input[name="password"]`).MustWaitVisible()

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

	page.MustWaitNavigation()

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

// parseTweetDetailResponse parses the API response and extracts tweet data.
func (e *TwitterExtractor) parseTweetDetailResponse(apiResponse *TweetDetailResponse) (*TwitterData, error) {
	tweetData := &TwitterData{
		Comments: []TwitterComment{},
	}

	for _, instruction := range apiResponse.Data.ThreadedConversationWithInjectionsV2.Instructions {
		if instruction.Type == "TimelineAddEntries" {
			for _, entry := range instruction.Entries {
				if strings.HasPrefix(entry.EntryID, "tweet-") {
					tweetResult := entry.Content.ItemContent.TweetResults.Result
					if tweetResult.Typename == "Tweet" {
						tweetData.TweetContent = tweetResult.Legacy.FullText
						if tweetResult.Core.UserResults.Result.Legacy.Name != "" {
							tweetData.TweetAuthor = fmt.Sprintf("%s (@%s)", tweetResult.Core.UserResults.Result.Legacy.Name, tweetResult.Core.UserResults.Result.Legacy.ScreenName)
						} else {
							tweetData.TweetAuthor = "Unknown Author"
						}
					}
				} else if strings.HasPrefix(entry.EntryID, "conversationthread-") {
					for _, item := range entry.Content.Items {
						tweetResult := item.Item.ItemContent.TweetResults.Result
						if tweetResult.Typename == "Tweet" {
							comment := TwitterComment{
								Author:    tweetResult.Core.UserResults.Result.Legacy.Name,
								Username:  "@" + tweetResult.Core.UserResults.Result.Legacy.ScreenName,
								Content:   tweetResult.Legacy.FullText,
								Timestamp: tweetResult.Legacy.CreatedAt,
								Likes:     fmt.Sprintf("%d", tweetResult.Legacy.FavoriteCount),
								Replies:   fmt.Sprintf("%d", tweetResult.Legacy.ReplyCount),
								Retweets:  fmt.Sprintf("%d", tweetResult.Legacy.RetweetCount),
							}
							if comment.Author == "" {
								comment.Author = "Unknown"
							}
							tweetData.Comments = append(tweetData.Comments, comment)
						}
					}
				}
			}
		}
	}

	tweetData.TotalComments = len(tweetData.Comments)

	if tweetData.TweetContent == "" {
		return nil, fmt.Errorf("could not find main tweet content in the API response")
	}

	return tweetData, nil
}

// saveCookies saves browser cookies to a file
func (e *TwitterExtractor) saveCookies(page *rod.Page, filename string) error {
	cookies, err := page.Cookies(nil)
	if err != nil {
		return fmt.Errorf("could not get cookies: %w", err)
	}

	jsonData, err := json.MarshalIndent(cookies, "", "  ")
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
