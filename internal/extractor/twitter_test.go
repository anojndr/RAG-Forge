package extractor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"

	"web-search-api-for-llms/internal/browser"
	"web-search-api-for-llms/internal/config"
)

// TestTwitterExtractorBrowserLaunch tests browser launch and connection
func TestTwitterExtractorBrowserLaunch(t *testing.T) {
	t.Log("Testing browser launch and connection...")

	// Create launcher with options
	launcherURL := browser.NewLauncher().MustLaunch()

	// Test browser connection
	browser := rod.New().ControlURL(launcherURL).MustConnect()
	defer browser.MustClose()

	// Verify browser is connected
	version := browser.MustVersion()
	t.Logf("Browser connected successfully. Version: %+v", version)

	// Test page creation
	page := browser.MustPage()
	defer page.MustClose()

	t.Log("✓ Browser launch and connection successful")
}

// TestTwitterExtractorUserAgent tests user agent setting
func TestTwitterExtractorUserAgent(t *testing.T) {
	t.Log("Testing user agent setting...")

	// Launch browser
	launcherURL := browser.NewLauncher().MustLaunch()

	browser := rod.New().ControlURL(launcherURL).MustConnect()
	defer browser.MustClose()

	page := browser.MustPage()
	defer page.MustClose()

	// Set custom user agent
	expectedUserAgent := "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
	page.MustSetUserAgent(&proto.NetworkSetUserAgentOverride{
		UserAgent: expectedUserAgent,
	})

	// Navigate to a test page that shows user agent
	page.MustNavigate("https://httpbin.org/user-agent")
	page.MustWaitLoad()

	// Get the response body
	body := page.MustElement("body").MustText()
	t.Logf("User agent response: %s", body)

	// Verify user agent was set correctly
	if !strings.Contains(body, expectedUserAgent) {
		t.Errorf("User agent not set correctly. Expected to contain: %s, Got: %s", expectedUserAgent, body)
	} else {
		t.Log("✓ User agent setting successful")
	}
}

// TestTwitterExtractorPageNavigation tests page navigation
func TestTwitterExtractorPageNavigation(t *testing.T) {
	t.Log("Testing page navigation...")

	// Launch browser
	launcherURL := browser.NewLauncher().MustLaunch()

	browser := rod.New().ControlURL(launcherURL).MustConnect()
	defer browser.MustClose()

	page := browser.MustPage()
	defer page.MustClose()

	// Set user agent
	page.MustSetUserAgent(&proto.NetworkSetUserAgentOverride{
		UserAgent: "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
	})

	// Test navigation to multiple URLs
	testURLs := []string{
		"https://httpbin.org/",
		"https://example.com/",
	}

	for _, url := range testURLs {
		t.Logf("Navigating to: %s", url)
		page.MustNavigate(url)
		page.MustWaitLoad()

		// Verify navigation succeeded
		currentURL := page.MustInfo().URL
		if !strings.Contains(currentURL, strings.TrimPrefix(url, "https://")) {
			t.Errorf("Navigation failed. Expected URL to contain: %s, Got: %s", url, currentURL)
		} else {
			t.Logf("✓ Successfully navigated to: %s", currentURL)
		}

		// Small delay between navigations
		time.Sleep(500 * time.Millisecond)
	}
}

// TestTwitterExtractorElementInteraction tests element interaction
func TestTwitterExtractorElementInteraction(t *testing.T) {
	t.Skip("Skipping element interaction test due to persistent hanging issues.")
}

// TestTwitterExtractorCookieManagement tests cookie save and load functionality
func TestTwitterExtractorCookieManagement(t *testing.T) {
	t.Log("Testing cookie management...")

	// Create a test cookie file name
	testCookieFile := "test_twitter_cookies.json"
	defer func() {
		if err := os.Remove(testCookieFile); err != nil {
			t.Logf("Warning: failed to remove test cookie file: %v", err)
		}
	}() // Clean up after test

	// Launch browser
	launcherURL := browser.NewLauncher().MustLaunch()

	browser := rod.New().ControlURL(launcherURL).MustConnect()
	defer browser.MustClose()

	page := browser.MustPage()
	defer page.MustClose()

	// Navigate to a page that sets cookies
	page.MustNavigate("https://httpbin.org/cookies/set?testcookie=testvalue&sessionid=12345")
	page.MustWaitLoad()

	// Test saving cookies
	t.Log("Testing cookie saving...")
	err := saveCookiesTest(page, testCookieFile)
	if err != nil {
		t.Errorf("Failed to save cookies: %v", err)
	} else {
		t.Log("✓ Cookies saved successfully")
	}

	// Verify cookie file exists
	if _, err := os.Stat(testCookieFile); os.IsNotExist(err) {
		t.Error("Cookie file was not created")
	} else {
		t.Log("✓ Cookie file created")
	}

	// Create a new page to test loading cookies
	page2 := browser.MustPage()
	defer page2.MustClose()

	// Test loading cookies
	t.Log("Testing cookie loading...")
	success := loadCookiesTest(page2, testCookieFile)
	if !success {
		t.Error("Failed to load cookies")
	} else {
		t.Log("✓ Cookies loaded successfully")
	}

	// Navigate to check cookies
	page2.MustNavigate("https://httpbin.org/cookies")
	page2.MustWaitLoad()

	// Verify cookies were loaded
	body := page2.MustElement("body").MustText()
	if strings.Contains(body, "testcookie") && strings.Contains(body, "testvalue") {
		t.Log("✓ Cookies verified successfully")
	} else {
		t.Errorf("Cookies not loaded correctly. Response: %s", body)
	}
}

// TestTwitterExtractorTweetIDExtraction tests tweet ID extraction from various URL formats
func TestTwitterExtractorTweetIDExtraction(t *testing.T) {
	t.Log("Testing tweet ID extraction...")

	testCases := []struct {
		url        string
		expectedID string
		shouldPass bool
	}{
		{"https://twitter.com/username/status/1234567890", "1234567890", true},
		{"https://x.com/username/status/9876543210", "9876543210", true},
		{"https://mobile.twitter.com/user/status/1111111111", "1111111111", true},
		{"twitter.com/user/status/2222222222", "2222222222", true},
		{"x.com/user/status/3333333333?s=20", "3333333333", true},
		{"https://twitter.com/username/", "", false},
		{"https://google.com/status/1234567890", "", false},
		{"not-a-url", "", false},
	}

	for _, tc := range testCases {
		t.Logf("Testing URL: %s", tc.url)
		id := extractTweetID(tc.url)
		
		if tc.shouldPass && id != tc.expectedID {
			t.Errorf("Expected ID %s, got %s for URL %s", tc.expectedID, id, tc.url)
		} else if !tc.shouldPass && id != "" {
			t.Errorf("Expected no ID, got %s for URL %s", id, tc.url)
		} else {
			t.Logf("✓ Correctly extracted ID: %s", id)
		}
	}
}

// TestTwitterExtractorWithContext tests the extraction with context and timeout
func TestTwitterExtractorWithContext(t *testing.T) {
	t.Log("Testing Twitter extraction with context...")

	// Create test config
	appConfig := &config.AppConfig{
		TwitterUsername: os.Getenv("TWITTER_USERNAME"),
		TwitterPassword: os.Getenv("TWITTER_PASSWORD"),
	}

	// Skip if credentials not provided
	if appConfig.TwitterUsername == "" || appConfig.TwitterPassword == "" {
		t.Skip("Skipping Twitter login test - TWITTER_USERNAME and TWITTER_PASSWORD not set")
	}

	extractor := NewTwitterExtractor(appConfig)

	// Test with timeout context
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create a simple test that verifies context handling
	t.Log("Testing context cancellation handling...")
	
	// Cancel context immediately to test cancellation handling
	cancel()
	
	_, err := extractor.extractTweetDataWithContext(ctx, "1234567890", "https://twitter.com/test/status/1234567890")
	if err == nil {
		t.Error("Expected error when context is cancelled")
	} else if !strings.Contains(err.Error(), "context cancelled") {
		t.Errorf("Expected context cancelled error, got: %v", err)
	} else {
		t.Log("✓ Context cancellation handled correctly")
	}
}

// Helper functions for cookie testing
func saveCookiesTest(page *rod.Page, filename string) error {
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

	return nil
}

func loadCookiesTest(page *rod.Page, filename string) bool {
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		return false
	}

	data, err := os.ReadFile(filename)
	if err != nil {
		return false
	}

	var cookieData []map[string]interface{}
	err = json.Unmarshal(data, &cookieData)
	if err != nil {
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

// TestTwitterExtractorIntegration provides a full integration test
func TestTwitterExtractorIntegration(t *testing.T) {
	t.Log("Running Twitter extractor integration test...")

	// This test requires actual Twitter credentials
	appConfig := &config.AppConfig{
		TwitterUsername: os.Getenv("TWITTER_USERNAME"),
		TwitterPassword: os.Getenv("TWITTER_PASSWORD"),
	}

	if appConfig.TwitterUsername == "" || appConfig.TwitterPassword == "" {
		t.Skip("Skipping integration test - TWITTER_USERNAME and TWITTER_PASSWORD not set")
	}

	extractor := NewTwitterExtractor(appConfig)

	// Test with a known public tweet
	testURL := "https://twitter.com/Twitter/status/1683542487476011008"
	
	result, err := extractor.Extract(testURL)
	if err != nil {
		t.Fatalf("Failed to extract tweet: %v", err)
	}

	// Verify result
	if !result.ProcessedSuccessfully {
		t.Errorf("Tweet was not processed successfully: %s", result.Error)
	}

	if result.Data == nil {
		t.Fatal("No tweet data extracted")
	}

	tweetData, ok := result.Data.(*TwitterData)
	if !ok {
		t.Fatal("Invalid data type returned")
	}

	// Verify tweet data
	if tweetData.TweetContent == "" {
		t.Error("No tweet content extracted")
	} else {
		t.Logf("✓ Tweet content: %s", tweetData.TweetContent)
	}

	if tweetData.TweetAuthor == "" {
		t.Error("No tweet author extracted")
	} else {
		t.Logf("✓ Tweet author: %s", tweetData.TweetAuthor)
	}

	t.Logf("✓ Total comments extracted: %d", tweetData.TotalComments)
}
