package extractor

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	jsoniter "github.com/json-iterator/go"

	"web-search-api-for-llms/internal/browser"
	"web-search-api-for-llms/internal/config"
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

// TwitterProfileResult holds the formatted result for a profile URL extraction.
type TwitterProfileResult struct {
	ProfileURL   string         `json:"profile_url"`
	LatestTweets []TweetExtract `json:"latest_tweets"`
}

// TweetExtract holds the URL and the extracted data for a single tweet.
type TweetExtract struct {
	URL  string       `json:"url"`
	Data *TwitterData `json:"data"`
}

// TwitterExtractor implements the Extractor interface for Twitter/X URLs
type TwitterExtractor struct {
	BaseExtractor
	BrowserPool *browser.Pool
	Config      *config.AppConfig
	cookieMutex sync.RWMutex // Mutex for cookie file access
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

func (e *TwitterExtractor) Extract(targetURL string, endpoint string, maxChars *int, result *ExtractedResult) error {
	slog.Info("TwitterExtractor: Starting extraction", "url", targetURL)
	result.SourceType = "twitter"

	// Create a timeout context for the entire extraction
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Check if we have Twitter credentials
	if e.Config.TwitterUsername == "" || e.Config.TwitterPassword == "" {
		return fmt.Errorf("twitter credentials not configured")
	}

	if isProfileURL(targetURL) {
		// Handle profile URL
		if endpoint != "/extract" {
			return fmt.Errorf("twitter profile URL extraction is only available on the /extract endpoint")
		}
		return e.extractFromProfileURL(ctx, targetURL, maxChars, result)
	}

	// Handle single tweet URL (existing logic)
	tweetID := extractTweetID(targetURL)
	if tweetID == "" {
		return fmt.Errorf("could not extract tweet ID from URL")
	}

	slog.Debug("TwitterExtractor: Extracted Tweet ID", "tweet_id", tweetID, "url", targetURL)

	// Extract tweet data using browser automation with context
	tweetData, err := e.extractTweetDataWithContext(ctx, tweetID, targetURL)
	if err != nil {
		return fmt.Errorf("extraction failed: %w", err)
	}

	result.Data = tweetData
	result.ProcessedSuccessfully = true

	if maxChars != nil {
		if data, ok := result.Data.(*TwitterData); ok {
			data.TweetContent = truncateText(data.TweetContent, *maxChars)

			// Truncate comments as well
			remainingChars := *maxChars - len(data.TweetContent)
			if remainingChars > 0 {
				var truncatedComments []TwitterComment
				for _, comment := range data.Comments {
					if remainingChars <= 0 {
						break
					}
					if len(comment.Content) > remainingChars {
						comment.Content = comment.Content[:remainingChars]
					}
					truncatedComments = append(truncatedComments, comment)
					remainingChars -= len(comment.Content)
				}
				data.Comments = truncatedComments
			} else {
				data.Comments = []TwitterComment{}
			}

			result.Data = data
		}
	}

	slog.Info("TwitterExtractor: Successfully extracted tweet data", "url", targetURL)
	return nil
}

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
			var tweetIDRegex = regexp.MustCompile(`^\d+$`)
			if tweetIDRegex.MatchString(tweetID) {
				return tweetID
			}
		}
	}

	return ""
}

// isProfileURL checks if a URL is a Twitter profile URL.
func isProfileURL(tweetURL string) bool {
	// A simple heuristic: if it's a valid Twitter domain and doesn't contain "/status/",
	// it's likely a profile URL.
	parsedURL, err := url.Parse(tweetURL)
	if err != nil {
		return false
	}
	return IsTwitterDomain(parsedURL.Hostname()) && !strings.Contains(parsedURL.Path, "/status/")
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
		slog.Info("TwitterExtractor: Loaded saved session cookies")
		// Test if we're still logged in by navigating to the home page with a timeout
		slog.Debug("TwitterExtractor: Navigating to x.com/home to check session status")
		err := page.Timeout(5 * time.Second).Navigate("https://x.com/home")
		if err != nil {
			slog.Warn("TwitterExtractor: Failed to navigate to home page to check session, assuming session is expired and logging in.", "error", err)
			if loginErr := e.loginToTwitter(page); loginErr != nil {
				return nil, fmt.Errorf("login failed: %w", loginErr)
			}
			if saveErr := e.saveCookies(page, cookiesFile); saveErr != nil {
				slog.Warn("TwitterExtractor: Failed to save cookies", "error", saveErr)
			}
		} else {
			page.MustWaitNavigation()

			currentURL := page.MustInfo().URL
			if strings.Contains(currentURL, "/home") {
				slog.Info("TwitterExtractor: Session is still valid, skipping login")
			} else {
				slog.Info("TwitterExtractor: Session expired, logging in")
				if loginErr := e.loginToTwitter(page); loginErr != nil {
					return nil, fmt.Errorf("login failed: %w", loginErr)
				}
				if saveErr := e.saveCookies(page, cookiesFile); saveErr != nil {
					slog.Warn("TwitterExtractor: Failed to save cookies", "error", saveErr)
				}
			}
		}
	} else {
		slog.Info("TwitterExtractor: No saved session found, logging in")
		if err := e.loginToTwitter(page); err != nil {
			return nil, fmt.Errorf("login failed: %w", err)
		}
		if err := e.saveCookies(page, cookiesFile); err != nil {
			slog.Warn("TwitterExtractor: Failed to save cookies", "error", err)
		}
	}

	// Use a channel to receive the API response
	apiResponseChan := make(chan *TweetDetailResponse)
	errChan := make(chan error, 1)

	// Set up request hijacking
	router := page.HijackRequests()
	defer func() {
		if err := router.Stop(); err != nil {
			slog.Warn("TwitterExtractor: Error stopping router", "error", err)
		}
	}()

	router.MustAdd("*TweetDetail*", func(ctx *rod.Hijack) {
		err := ctx.LoadResponse(http.DefaultClient, true)
		if err != nil {
			errChan <- fmt.Errorf("could not load response: %w", err)
			return
		}

		var apiResponse TweetDetailResponse
		json := jsoniter.ConfigCompatibleWithStandardLibrary
		if err := json.Unmarshal(ctx.Response.Payload().Body, &apiResponse); err != nil {
			errChan <- fmt.Errorf("error parsing TweetDetail JSON: %v", err)
			return
		}
		apiResponseChan <- &apiResponse
	})

	go router.Run()

	// Navigate to the tweet
	slog.Debug("TwitterExtractor: Navigating to tweet", "url", tweetURL)
	if err := page.Navigate(tweetURL); err != nil {
		return nil, fmt.Errorf("failed to navigate to tweet: %w", err)
	}

	// Wait for the API response or timeout
	select {
	case apiResponse := <-apiResponseChan:
		slog.Info("TwitterExtractor: Successfully captured TweetDetail API response")
		return e.parseTweetDetailResponse(apiResponse)
	case err := <-errChan:
		return nil, err
	case <-time.After(15 * time.Second):
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
	slog.Debug("TwitterExtractor: Clicking Next button")
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
	slog.Debug("TwitterExtractor: Entering password")
	passwordField := page.MustElement(`input[name="password"]`)
	passwordField.MustSelectAllText().MustInput(e.Config.TwitterPassword)

	// Click Log in button
	slog.Debug("TwitterExtractor: Clicking Log in button")
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
	slog.Info("TwitterExtractor: Login successful", "url", currentURL)

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
	e.cookieMutex.Lock()
	defer e.cookieMutex.Unlock()

	cookies, err := page.Cookies(nil)
	if err != nil {
		return fmt.Errorf("could not get cookies: %w", err)
	}

	// Use jsoniter for performance
	json := jsoniter.ConfigCompatibleWithStandardLibrary
	jsonData, err := json.MarshalIndent(cookies, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal cookies: %w", err)
	}

	err = os.WriteFile(filename, jsonData, 0644)
	if err != nil {
		return fmt.Errorf("failed to save cookies: %w", err)
	}

	slog.Info("TwitterExtractor: Session cookies saved", "filename", filename)
	return nil
}

// loadCookies loads browser cookies from a file
func (e *TwitterExtractor) loadCookies(page *rod.Page, filename string) bool {
	e.cookieMutex.RLock()
	defer e.cookieMutex.RUnlock()

	if _, err := os.Stat(filename); os.IsNotExist(err) {
		return false
	}

	data, err := os.ReadFile(filename)
	if err != nil {
		slog.Warn("TwitterExtractor: Could not read cookies file", "error", err)
		return false
	}

	// Use jsoniter for performance
	json := jsoniter.ConfigCompatibleWithStandardLibrary
	var cookieData []map[string]interface{}
	err = json.Unmarshal(data, &cookieData)
	if err != nil {
		slog.Warn("TwitterExtractor: Could not parse cookies", "error", err)
		return false
	}

	var cookies []*proto.NetworkCookieParam
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
		cookies = append(cookies, cookie)
	}
	page.MustSetCookies(cookies...)
	return true
}

// extractFromProfileURL handles the extraction of the latest 5 tweets from a profile URL.
func (e *TwitterExtractor) extractFromProfileURL(ctx context.Context, profileURL string, maxChars *int, result *ExtractedResult) error {
	result.SourceType = "twitter_profile"

	tweetURLs, err := e.extractTweetURLsFromProfile(ctx, profileURL)
	if err != nil {
		return fmt.Errorf("failed to extract tweet URLs from profile: %w", err)
	}

	var wg sync.WaitGroup
	tweetExtracts := make(chan TweetExtract, len(tweetURLs))

	for _, tweetURL := range tweetURLs {
		wg.Add(1)
		go func(tURL string) {
			defer wg.Done()
			tweetID := extractTweetID(tURL)
			if tweetID == "" {
				slog.Warn("TwitterExtractor: Could not extract tweet ID", "url", tURL)
				return
			}

			tweetData, err := e.extractTweetDataWithContext(ctx, tweetID, tURL)
			if err != nil {
				slog.Error("TwitterExtractor: Failed to extract data for tweet", "url", tURL, "error", err)
				return
			}
			tweetExtracts <- TweetExtract{URL: tURL, Data: tweetData}
		}(tweetURL)
	}

	wg.Wait()
	close(tweetExtracts)

	profileResult := &TwitterProfileResult{
		ProfileURL:   profileURL,
		LatestTweets: []TweetExtract{},
	}

	for extract := range tweetExtracts {
		profileResult.LatestTweets = append(profileResult.LatestTweets, extract)
	}

	result.Data = profileResult
	result.ProcessedSuccessfully = true

	slog.Info("TwitterExtractor: Successfully extracted latest tweets from profile", "url", profileURL)
	return nil
}

// extractTweetURLsFromProfile extracts the latest 5 tweet URLs from a profile page.
func (e *TwitterExtractor) extractTweetURLsFromProfile(ctx context.Context, profileURL string) ([]string, error) {
	browser := e.BrowserPool.Get()
	defer e.BrowserPool.Return(browser)

	page, err := browser.Page(proto.TargetCreateTarget{URL: ""})
	if err != nil {
		return nil, fmt.Errorf("failed to create page: %w", err)
	}
	defer page.MustClose()

	if err := page.Navigate(profileURL); err != nil {
		return nil, fmt.Errorf("failed to navigate to profile page: %w", err)
	}

	page.MustWaitLoad()
	// Wait for the <article> element to be present, which contains tweets.
	page.MustElement("article").MustWaitVisible()

	articles, err := page.Elements("article")
	if err != nil {
		return nil, fmt.Errorf("could not find tweet articles: %w", err)
	}

	var tweetURLs []string
	for _, article := range articles {
		if len(tweetURLs) >= 5 {
			break
		}
		link, err := article.Element(`a[href*="/status/"]`)
		if err != nil {
			continue
		}
		href, err := link.Attribute("href")
		if err != nil {
			continue
		}
		tweetURLs = append(tweetURLs, "https://x.com"+*href)
	}

	if len(tweetURLs) == 0 {
		return nil, fmt.Errorf("no tweet URLs found on profile page")
	}

	return tweetURLs, nil
}
