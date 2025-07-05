package config

import (
	"fmt"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

// AppConfig holds all configuration for the application
type AppConfig struct {
	YouTubeAPIKey        string
	RedditClientID       string
	RedditClientSecret   string
	RedditUserAgent      string
	SearxNGURL           string
	SerperAPIKey         string
	SerperAPIURL         string
	MainSearchEngine     string
	FallbackSearchEngine string
	Port                 string
	// Webshare proxy credentials for YouTube transcript API
	WebshareProxyUsername string
	WebshareProxyPassword string
	// Comma-separated order for transcript extraction methods: ytapi, tactiq
	TranscriptOrder string
	// Twitter/X credentials for content extraction
	TwitterUsername string
	TwitterPassword string
}

// LoadConfig loads configuration from .env file and environment variables
func LoadConfig() (*AppConfig, error) {
	// Attempt to load .env file. If it doesn't exist, that's fine,
	// environment variables can still be used.
	if err := godotenv.Load(); err != nil {
		// Log but don't fail - environment variables might be set directly
		// This is common in containerized environments
		fmt.Printf("Info: Could not load .env file: %v (this is ok if using environment variables)\n", err)
	}

	config := &AppConfig{
		YouTubeAPIKey:         os.Getenv("YOUTUBE_API_KEY"),
		RedditClientID:        os.Getenv("REDDIT_CLIENT_ID"),
		RedditClientSecret:    os.Getenv("REDDIT_CLIENT_SECRET"),
		RedditUserAgent:       os.Getenv("REDDIT_USER_AGENT"),
		SearxNGURL:            getEnv("SEARXNG_URL", "http://localhost:18088"),
		SerperAPIKey:          os.Getenv("SERPER_API_KEY"),
		SerperAPIURL:          getEnv("SERPER_API_URL", "https://google.serper.dev/search"),
		MainSearchEngine:      getEnv("MAIN_SEARCH_ENGINE", "searxng"),
		FallbackSearchEngine:  getEnv("FALLBACK_SEARCH_ENGINE", "serper"),
		Port:                  getEnv("PORT", "8080"),
		WebshareProxyUsername: os.Getenv("WEBSHARE_PROXY_USERNAME"),
		WebshareProxyPassword: os.Getenv("WEBSHARE_PROXY_PASSWORD"),
		TranscriptOrder:       getEnv("YOUTUBE_TRANSCRIPT_ORDER", "ytapi,tactiq"),
		TwitterUsername:       os.Getenv("TWITTER_USERNAME"),
		TwitterPassword:       os.Getenv("TWITTER_PASSWORD"),
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	return config, nil
}

// Validate checks that the configuration is valid
func (c *AppConfig) Validate() error {
	// Validate port is a valid number
	if _, err := strconv.Atoi(c.Port); err != nil {
		return fmt.Errorf("invalid port number: %s", c.Port)
	}

	// Validate search engine options
	validEngines := map[string]bool{
		"searxng": true,
		"serper":  true,
		"":        true, // Empty is allowed for fallback
	}

	if !validEngines[c.MainSearchEngine] {
		return fmt.Errorf("invalid main search engine: %s (must be 'searxng' or 'serper')", c.MainSearchEngine)
	}

	if !validEngines[c.FallbackSearchEngine] {
		return fmt.Errorf("invalid fallback search engine: %s (must be 'searxng', 'serper', or empty)", c.FallbackSearchEngine)
	}

	// Warn about missing optional configurations
	if c.YouTubeAPIKey == "" {
		fmt.Println("Warning: YOUTUBE_API_KEY not set - YouTube features will be limited")
	}

	if c.RedditClientID == "" || c.RedditClientSecret == "" {
		fmt.Println("Warning: Reddit API credentials not set - Reddit features will be limited")
	}

	if c.TwitterUsername == "" || c.TwitterPassword == "" {
		fmt.Println("Warning: Twitter credentials not set - Twitter/X features will be limited")
	}

	// Warn about incomplete Webshare proxy credentials
	if (c.WebshareProxyUsername != "" && c.WebshareProxyPassword == "") || (c.WebshareProxyUsername == "" && c.WebshareProxyPassword != "") {
		fmt.Println("Warning: Incomplete Webshare proxy credentials - proxy will not be used")
	}

	return nil
}

// GetPort returns the port as an integer
func (c *AppConfig) GetPort() int {
	port, _ := strconv.Atoi(c.Port) // Already validated in Validate()
	return port
}

// HasYouTubeConfig returns true if YouTube API configuration is available
func (c *AppConfig) HasYouTubeConfig() bool {
	return c.YouTubeAPIKey != ""
}

// HasRedditConfig returns true if Reddit API configuration is available
func (c *AppConfig) HasRedditConfig() bool {
	return c.RedditClientID != "" && c.RedditClientSecret != ""
}

// HasSerperConfig returns true if Serper API configuration is available
func (c *AppConfig) HasSerperConfig() bool {
	return c.SerperAPIKey != ""
}

// HasTwitterConfig returns true if Twitter credentials are available
func (c *AppConfig) HasTwitterConfig() bool {
	return c.TwitterUsername != "" && c.TwitterPassword != ""
}

// getEnv gets an environment variable or returns a default value
func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}
