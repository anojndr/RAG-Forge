package extractor

import (
	nethttp "net/http" // Aliased http import

	"web-search-api-for-llms/internal/config"
)

// ExtractedResult represents the common structure for data extracted from any source.
// Specific extractors will populate the Data field with their unique structures.
type ExtractedResult struct {
	URL                   string      `json:"url"`
	SourceType            string      `json:"source_type"`
	ProcessedSuccessfully bool        `json:"processed_successfully"`
	Data                  interface{} `json:"data,omitempty"` // Can be YouTubeData, RedditData, PDFData, WebpageData
	Error                 string      `json:"error,omitempty"`
}

// Specific data structures for each source type

// YouTubeData represents extracted data from YouTube videos
type YouTubeData struct {
	Title       string        `json:"title"`
	ChannelName string        `json:"channel_name"`
	Comments    []interface{} `json:"comments"`
	Transcript  string        `json:"transcript"`
}

// RedditData represents extracted data from Reddit posts
type RedditData struct {
	PostTitle string        `json:"post_title"`
	PostBody  string        `json:"post_body"`
	Score     int           `json:"score"`
	Author    string        `json:"author"`
	Comments  []interface{} `json:"comments"`
}

// PDFData represents extracted data from PDF documents
type PDFData struct {
	TextContent string `json:"text_content"`
}

// WebpageData represents extracted data from general web pages
type WebpageData struct {
	TextContent string `json:"text_content"`
	Title       string `json:"title,omitempty"`
}

// TwitterData represents extracted data from Twitter/X posts
type TwitterData struct {
	TweetContent  string           `json:"tweet_content"`
	TweetAuthor   string           `json:"tweet_author"`
	Comments      []TwitterComment `json:"comments"`
	TotalComments int              `json:"total_comments"`
}

// TwitterComment represents a comment/reply on a Twitter/X post
type TwitterComment struct {
	Author    string `json:"author"`
	Username  string `json:"username"`
	Content   string `json:"content"`
	Timestamp string `json:"timestamp"`
	Likes     string `json:"likes"`
	Replies   string `json:"replies"`
	Retweets  string `json:"retweets"`
}

// ContentExtractor defines the interface for content extractors.
// This interface is kept small and focused on a single responsibility.
type ContentExtractor interface {
	// Extract processes a URL and returns extracted content or an error
	Extract(url string) (*ExtractedResult, error)
}

// URLClassifier defines the interface for URL classification
type URLClassifier interface {
	// ClassifyURL determines the type of content at a URL
	ClassifyURL(url string) (string, error)
}

// Configurable defines the interface for components that need configuration
type Configurable interface {
	// GetConfig returns the component's configuration
	GetConfig() *config.AppConfig
}

// HealthChecker defines the interface for components that can report their health
type HealthChecker interface {
	// HealthCheck returns nil if the component is healthy, error otherwise
	HealthCheck() error
}

// Extractor is a composite interface that combines the main extraction capability
// This maintains backward compatibility while promoting the use of smaller interfaces
type Extractor interface {
	ContentExtractor
}

// BaseExtractor provides common functionality for all extractors
type BaseExtractor struct {
	Config     *config.AppConfig
	HTTPClient *nethttp.Client
}

// NewBaseExtractor creates a common base for extractors
func NewBaseExtractor(cfg *config.AppConfig) BaseExtractor {
	return BaseExtractor{
		Config: cfg,
		HTTPClient: &nethttp.Client{
			Timeout: 10 * nethttp.DefaultClient.Timeout, // 10 seconds default timeout
		},
	}
}

// GetConfig implements the Configurable interface
func (be *BaseExtractor) GetConfig() *config.AppConfig {
	return be.Config
}

// HealthCheck implements the HealthChecker interface
func (be *BaseExtractor) HealthCheck() error {
	if be.Config == nil {
		return nethttp.ErrServerClosed // Use a known error type
	}
	return nil
}
