package extractor

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"web-search-api-for-llms/internal/config"
	"web-search-api-for-llms/internal/logger"
	"web-search-api-for-llms/internal/useragent"
)

// RedditExtractor implements the Extractor interface for Reddit URLs.
type RedditExtractor struct {
	BaseExtractor
	accessToken string
	tokenExpiry time.Time
}

// NewRedditExtractor creates a new RedditExtractor.
func NewRedditExtractor(appConfig *config.AppConfig, client *http.Client) *RedditExtractor {
	return &RedditExtractor{
		BaseExtractor: NewBaseExtractor(appConfig, client),
	}
}

// Reddit API response structures
type RedditAPIResponse struct {
	Data struct {
		Children []struct {
			Data json.RawMessage `json:"data"`
		} `json:"children"`
	} `json:"data"`
}

type RedditPost struct {
	Title     string `json:"title"`
	Selftext  string `json:"selftext"`
	Score     int    `json:"score"`
	Author    string `json:"author"`
	URL       string `json:"url"`
	ID        string `json:"id"`
	Subreddit string `json:"subreddit"`
}

type RedditCommentsResponse struct {
	Data struct {
		Children []struct {
			Data RedditComment `json:"data"`
		} `json:"children"`
	} `json:"data"`
}

// RedditReplies represents the nested replies in a Reddit comment.
type RedditReplies struct {
	Data struct {
		Children []struct {
			RedditComment
		} `json:"children"`
	} `json:"data"`
}

// UnmarshalJSON handles the case where "replies" can be an empty string.
func (r *RedditReplies) UnmarshalJSON(b []byte) error {
	if string(b) == `""` {
		return nil
	}

	// Use an alias to avoid recursion
	type Alias RedditReplies
	var a Alias
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}
	*r = RedditReplies(a)
	return nil
}

// RedditComment represents a Reddit comment, which can be a regular comment or a "more" object.
type RedditComment struct {
	Kind    string        `json:"kind"`
	Body    string        `json:"body"`
	Author  string        `json:"author"`
	Score   int           `json:"score"`
	Replies RedditReplies `json:"replies"`
}

// OAuth token response
type RedditTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

// getAccessToken obtains an OAuth access token for Reddit API
func (e *RedditExtractor) getAccessToken() error {
	if e.Config.RedditClientID == "" || e.Config.RedditClientSecret == "" {
		return fmt.Errorf("reddit API credentials not configured")
	}

	// Check if we have a valid token
	if e.accessToken != "" && time.Now().Before(e.tokenExpiry) {
		return nil
	}

	// Request new token
	data := url.Values{}
	data.Set("grant_type", "client_credentials")

	req, err := http.NewRequest("POST", "https://www.reddit.com/api/v1/access_token", strings.NewReader(data.Encode()))
	if err != nil {
		return fmt.Errorf("failed to create token request: %v", err)
	}

	req.SetBasicAuth(e.Config.RedditClientID, e.Config.RedditClientSecret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	userAgent := e.Config.RedditUserAgent
	if userAgent == "" {
		userAgent = useragent.Random()
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := e.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to get access token: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Printf("RedditExtractor: Warning - failed to close response body: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("token request failed with status: %d", resp.StatusCode)
	}

	var tokenResp RedditTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return fmt.Errorf("failed to decode token response: %v", err)
	}

	e.accessToken = tokenResp.AccessToken
	e.tokenExpiry = time.Now().Add(time.Duration(tokenResp.ExpiresIn-60) * time.Second) // Refresh 1 minute early

	log.Printf("RedditExtractor: Successfully obtained access token")
	return nil
}

// RedditURLType represents the type of Reddit URL
type RedditURLType int

const (
	RedditPostURL RedditURLType = iota
	RedditSubredditURL
	RedditUserURL
	RedditCommentURL
	RedditSearchURL
)

// RedditURLInfo contains parsed information about a Reddit URL
type RedditURLInfo struct {
	Type      RedditURLType
	Subreddit string
	PostID    string
	CommentID string
	Username  string
	Query     string
}

// parseRedditURL parses a Reddit URL and returns detailed information about its type and components
func (e *RedditExtractor) parseRedditURL(redditURL string) (*RedditURLInfo, error) {
	parsedURL, err := url.Parse(redditURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %v", err)
	}

	pathParts := strings.Split(strings.Trim(parsedURL.Path, "/"), "/")

	if len(pathParts) < 2 {
		return nil, fmt.Errorf("invalid Reddit URL format: URL is too short")
	}

	info := &RedditURLInfo{}

	// Handle different Reddit URL formats
	switch pathParts[0] {
	case "r":
		if len(pathParts) < 2 {
			return nil, fmt.Errorf("invalid subreddit URL: missing subreddit name")
		}
		info.Subreddit = pathParts[1]

		if len(pathParts) == 2 {
			// /r/subreddit/
			info.Type = RedditSubredditURL
		} else if len(pathParts) >= 4 && pathParts[2] == "comments" {
			// /r/subreddit/comments/postid/title/
			info.Type = RedditPostURL
			info.PostID = pathParts[3]

			// Check if this is a specific comment
			if len(pathParts) >= 6 {
				info.Type = RedditCommentURL
				info.CommentID = pathParts[5]
			}
		} else if len(pathParts) >= 3 && pathParts[2] == "search" {
			// /r/subreddit/search/
			info.Type = RedditSearchURL
			info.Query = parsedURL.Query().Get("q")
		} else {
			return nil, fmt.Errorf("unsupported Reddit URL format: %s", redditURL)
		}

	case "u", "user":
		if len(pathParts) < 2 {
			return nil, fmt.Errorf("invalid user URL: missing username")
		}
		info.Type = RedditUserURL
		info.Username = pathParts[1]

	default:
		return nil, fmt.Errorf("unsupported Reddit URL format: must start with /r/, /u/, or /user/")
	}

	return info, nil
}

// fetchViaAPI attempts to fetch Reddit data using the official API with concurrent processing
func (e *RedditExtractor) fetchViaAPI(subreddit, postID string) (*ExtractedResult, error) {
	if err := e.getAccessToken(); err != nil {
		return nil, fmt.Errorf("failed to get access token: %v", err)
	}

	// Fetch post data
	postURL := fmt.Sprintf("https://oauth.reddit.com/r/%s/comments/%s", subreddit, postID)

	req, err := http.NewRequest("GET", postURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create API request: %v", err)
	}

	req.Header.Set("Authorization", "Bearer "+e.accessToken)
	userAgent := e.Config.RedditUserAgent
	if userAgent == "" {
		userAgent = useragent.Random()
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := e.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make API request: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Printf("RedditExtractor: Warning - failed to close response body: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API request failed with status: %d", resp.StatusCode)
	}

	var apiResponse []RedditAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResponse); err != nil {
		return nil, fmt.Errorf("failed to decode API response: %v", err)
	}

	if len(apiResponse) < 1 || len(apiResponse[0].Data.Children) == 0 {
		return nil, fmt.Errorf("no post data found in API response")
	}

	// Process post data and comments concurrently
	type processResult struct {
		post     *RedditPost
		comments []RedditComment
		err      error
	}

	resultsChan := make(chan processResult, 2)
	var wg sync.WaitGroup

	// Process post data
	wg.Add(1)
	go func() {
		defer wg.Done()
		var post RedditPost
		if err := json.Unmarshal(apiResponse[0].Data.Children[0].Data, &post); err != nil {
			resultsChan <- processResult{err: fmt.Errorf("failed to parse post data: %v", err)}
			return
		}
		resultsChan <- processResult{post: &post}
	}()

	// Process comments data
	wg.Add(1)
	go func() {
		defer wg.Done()
		var commentsData []RedditComment
		if len(apiResponse) > 1 {
			commentsData = e.extractCommentsFromAPI(apiResponse[1])
		}
		resultsChan <- processResult{comments: commentsData}
	}()

	// Wait for all goroutines to complete
	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	// Collect results
	var post *RedditPost
	var commentsData []RedditComment
	for result := range resultsChan {
		if result.err != nil {
			return nil, result.err
		}
		if result.post != nil {
			post = result.post
		}
		if result.comments != nil {
			commentsData = result.comments
		}
	}

	if post == nil {
		return nil, fmt.Errorf("failed to process post data")
	}

	result := &ExtractedResult{
		URL:                   fmt.Sprintf("https://www.reddit.com/r/%s/comments/%s", subreddit, postID),
		SourceType:            "reddit",
		ProcessedSuccessfully: true,
		Data: RedditData{
			PostTitle: post.Title,
			PostBody:  post.Selftext,
			Score:     post.Score,
			Author:    post.Author,
			Comments:  commentsData,
		},
	}

	return result, nil
}

// flattenReplies recursively extracts and flattens comment replies.
func (e *RedditExtractor) flattenReplies(children []struct{ RedditComment }) []RedditComment {
	var comments []RedditComment
	for _, child := range children {
		comment := child.RedditComment
		if comment.Kind == "more" || comment.Body == "" || comment.Body == "[deleted]" || comment.Body == "[removed]" {
			continue
		}

		// Get replies before clearing them to avoid deep nesting
		replies := comment.Replies.Data.Children
		comment.Replies.Data.Children = nil

		comments = append(comments, comment)

		// Recurse for nested replies
		if len(replies) > 0 {
			comments = append(comments, e.flattenReplies(replies)...)
		}
	}
	return comments
}

// extractCommentsFromAPI recursively extracts comments from Reddit API response
func (e *RedditExtractor) extractCommentsFromAPI(commentsResp RedditAPIResponse) []RedditComment {
	var comments []RedditComment

	for _, child := range commentsResp.Data.Children {
		var comment RedditComment
		if err := json.Unmarshal(child.Data, &comment); err != nil {
			log.Printf("RedditExtractor: Failed to unmarshal comment: %v", err)
			continue
		}

		// Skip "more" objects, empty, deleted, or removed comments
		if comment.Kind == "more" || comment.Body == "" || comment.Body == "[deleted]" || comment.Body == "[removed]" {
			continue
		}

		// Get replies before clearing them to avoid deep nesting
		replies := comment.Replies.Data.Children
		comment.Replies.Data.Children = nil

		comments = append(comments, comment)

		// Recursively extract and flatten replies
		if len(replies) > 0 {
			comments = append(comments, e.flattenReplies(replies)...)
		}

		// Limit to 50 comments for performance
		if len(comments) >= 50 {
			log.Printf("RedditExtractor: Reached comment limit of 50, stopping extraction")
			break
		}
	}

	log.Printf("RedditExtractor: Extracted %d comments", len(comments))
	return comments
}

// fetchSubredditPosts fetches recent posts from a subreddit
func (e *RedditExtractor) fetchSubredditPosts(subreddit string) (*ExtractedResult, error) {
	// Use .json endpoint for subreddit
	jsonURL := fmt.Sprintf("https://www.reddit.com/r/%s/.json?limit=10", subreddit)

	req, err := http.NewRequest("GET", jsonURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create subreddit request: %v", err)
	}

	userAgent := e.Config.RedditUserAgent
	if userAgent == "" {
		userAgent = useragent.Random()
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := e.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make subreddit request: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Printf("RedditExtractor: Warning - failed to close response body: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("subreddit request failed with status: %d", resp.StatusCode)
	}

	var jsonResponse RedditAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&jsonResponse); err != nil {
		return nil, fmt.Errorf("failed to decode subreddit JSON response: %v", err)
	}

	if len(jsonResponse.Data.Children) == 0 {
		return nil, fmt.Errorf("no posts found in subreddit")
	}

	// Extract posts data
	var posts []RedditPost
	for _, child := range jsonResponse.Data.Children {
		var post RedditPost
		if err := json.Unmarshal(child.Data, &post); err != nil {
			continue
		}
		posts = append(posts, post)
	}

	result := &ExtractedResult{
		URL:                   fmt.Sprintf("https://www.reddit.com/r/%s/", subreddit),
		SourceType:            "reddit",
		ProcessedSuccessfully: true,
		Data: RedditData{
			PostTitle: fmt.Sprintf("r/%s - Recent Posts", subreddit),
			PostBody:  fmt.Sprintf("Recent posts from r/%s", subreddit),
			Score:     0,
			Author:    "subreddit",
			Posts:     posts,
		},
	}

	return result, nil
}

// fetchUserPosts fetches recent posts from a user profile
func (e *RedditExtractor) fetchUserPosts(username string) (*ExtractedResult, error) {
	// Use .json endpoint for user posts
	jsonURL := fmt.Sprintf("https://www.reddit.com/user/%s/.json?limit=10", username)

	req, err := http.NewRequest("GET", jsonURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create user request: %v", err)
	}

	userAgent := e.Config.RedditUserAgent
	if userAgent == "" {
		userAgent = useragent.Random()
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := e.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make user request: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Printf("RedditExtractor: Warning - failed to close response body: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("user request failed with status: %d", resp.StatusCode)
	}

	var jsonResponse RedditAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&jsonResponse); err != nil {
		return nil, fmt.Errorf("failed to decode user JSON response: %v", err)
	}

	if len(jsonResponse.Data.Children) == 0 {
		return nil, fmt.Errorf("no posts found for user")
	}

	// Extract user posts data
	var posts []RedditPost
	for _, child := range jsonResponse.Data.Children {
		var post RedditPost
		if err := json.Unmarshal(child.Data, &post); err != nil {
			continue
		}
		posts = append(posts, post)
	}

	result := &ExtractedResult{
		URL:                   fmt.Sprintf("https://www.reddit.com/user/%s/", username),
		SourceType:            "reddit",
		ProcessedSuccessfully: true,
		Data: RedditData{
			PostTitle: fmt.Sprintf("u/%s - Recent Posts", username),
			PostBody:  fmt.Sprintf("Recent posts from u/%s", username),
			Score:     0,
			Author:    username,
			Posts:     posts,
		},
	}

	return result, nil
}

// fetchViaJSON attempts to fetch Reddit data using the .json fallback method
func (e *RedditExtractor) fetchViaJSON(redditURL string, maxChars *int) (*ExtractedResult, error) {
	// Add .json to the URL if not already present
	jsonURL := redditURL
	if !strings.HasSuffix(redditURL, ".json") {
		jsonURL = redditURL + ".json"
	}

	req, err := http.NewRequest("GET", jsonURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create JSON request: %v", err)
	}

	userAgent := e.Config.RedditUserAgent
	if userAgent == "" {
		userAgent = useragent.Random()
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := e.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make JSON request: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Printf("RedditExtractor: Warning - failed to close response body: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("JSON request failed with status: %d", resp.StatusCode)
	}

	var jsonResponse []RedditAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&jsonResponse); err != nil {
		return nil, fmt.Errorf("failed to decode JSON response: %v", err)
	}

	if len(jsonResponse) < 1 || len(jsonResponse[0].Data.Children) == 0 {
		return nil, fmt.Errorf("no post data found in JSON response")
	}

	// Extract post data
	var post RedditPost
	if err := json.Unmarshal(jsonResponse[0].Data.Children[0].Data, &post); err != nil {
		return nil, fmt.Errorf("failed to parse post data: %v", err)
	}

	// Extract comments data
	var commentsData []RedditComment
	if len(jsonResponse) > 1 {
		commentsData = e.extractCommentsFromAPI(jsonResponse[1])
	}

	result := &ExtractedResult{
		URL:                   redditURL,
		SourceType:            "reddit",
		ProcessedSuccessfully: true,
		Data: RedditData{
			PostTitle: post.Title,
			PostBody:  post.Selftext,
			Score:     post.Score,
			Author:    post.Author,
			Comments:  commentsData,
		},
	}

	if maxChars != nil {
		if data, ok := result.Data.(RedditData); ok {
			data.PostBody = truncateText(data.PostBody, *maxChars)
			
			// Truncate comments as well
			remainingChars := *maxChars - len(data.PostBody)
			if remainingChars > 0 {
				var truncatedComments []RedditComment
				for _, comment := range data.Comments {
					if remainingChars <= 0 {
						break
					}
					if len(comment.Body) > remainingChars {
						comment.Body = comment.Body[:remainingChars]
					}
					truncatedComments = append(truncatedComments, comment)
					remainingChars -= len(comment.Body)
				}
				data.Comments = truncatedComments
			} else {
				data.Comments = []RedditComment{}
			}
			
			result.Data = data
		}
	}

	return result, nil
}

// Extract attempts to fetch Reddit data using API first, then falls back to JSON method
func (e *RedditExtractor) Extract(redditURL string, maxChars *int) (*ExtractedResult, error) {
	log.Printf("RedditExtractor: Starting extraction for URL: %s", redditURL)

	result := &ExtractedResult{
		URL:        redditURL,
		SourceType: "reddit",
	}

	// Parse the Reddit URL to determine its type
	urlInfo, err := e.parseRedditURL(redditURL)
	if err != nil {
		result.Error = fmt.Sprintf("failed to parse Reddit URL: %v", err)
		logger.LogError("RedditExtractor: Error parsing URL %s: %s", redditURL, result.Error)
		return result, fmt.Errorf(result.Error)
	}

	log.Printf("RedditExtractor: URL type: %d for %s", urlInfo.Type, redditURL)

	// Handle different URL types
	switch urlInfo.Type {
	case RedditPostURL, RedditCommentURL:
		// Handle individual posts (comments are treated as posts with additional context)
		return e.extractPost(redditURL, urlInfo, maxChars)

	case RedditSubredditURL:
		// Handle subreddit feeds
		log.Printf("RedditExtractor: Extracting subreddit posts for r/%s", urlInfo.Subreddit)
		return e.fetchSubredditPosts(urlInfo.Subreddit)

	case RedditUserURL:
		// Handle user profiles
		log.Printf("RedditExtractor: Extracting user posts for u/%s", urlInfo.Username)
		return e.fetchUserPosts(urlInfo.Username)

	case RedditSearchURL:
		// Handle search results (not implemented yet)
		result.Error = "Reddit search URLs are not yet supported"
		logger.LogError("RedditExtractor: Search URLs not supported for %s", redditURL)
		return result, fmt.Errorf(result.Error)

	default:
		result.Error = "unsupported Reddit URL type"
		logger.LogError("RedditExtractor: Unsupported URL type for %s", redditURL)
		return result, fmt.Errorf(result.Error)
	}
}

// extractPost handles individual Reddit posts
func (e *RedditExtractor) extractPost(redditURL string, urlInfo *RedditURLInfo, maxChars *int) (*ExtractedResult, error) {
	result := &ExtractedResult{
		URL:        redditURL,
		SourceType: "reddit",
	}

	// First, try using the Reddit API
	if e.Config.RedditClientID != "" && e.Config.RedditClientSecret != "" {
		log.Printf("RedditExtractor: Attempting to use Reddit API for %s", redditURL)
		apiResult, err := e.fetchViaAPI(urlInfo.Subreddit, urlInfo.PostID)
		if err == nil {
			log.Printf("RedditExtractor: Successfully extracted data via API for %s", redditURL)
			if maxChars != nil {
				if data, ok := apiResult.Data.(RedditData); ok {
					if len(data.PostBody) > *maxChars {
						data.PostBody = data.PostBody[:*maxChars]
						apiResult.Data = data
					}
				}
			}
			return apiResult, nil
		}
		logger.LogError("RedditExtractor: API method failed for %s: %v. Falling back to JSON method", redditURL, err)
	} else {
		log.Printf("RedditExtractor: Reddit API credentials not configured. Using JSON fallback for %s", redditURL)
	}

	// Fallback to JSON method
	log.Printf("RedditExtractor: Attempting to use JSON method for %s", redditURL)
	jsonResult, err := e.fetchViaJSON(redditURL, maxChars)
	if err != nil {
		result.Error = fmt.Sprintf("both API and JSON methods failed: %v", err)
		logger.LogError("RedditExtractor: All methods failed for %s: %s", redditURL, result.Error)
		return result, fmt.Errorf(result.Error)
	}

	log.Printf("RedditExtractor: Successfully extracted data via JSON method for %s", redditURL)
	return jsonResult, nil
}

