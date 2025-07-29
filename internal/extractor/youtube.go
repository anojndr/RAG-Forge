package extractor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"web-search-api-for-llms/internal/config"
	"web-search-api-for-llms/internal/logger"
)

// YouTubeExtractor implements the Extractor interface for YouTube URLs.
type YouTubeExtractor struct {
	BaseExtractor
}

// NewYouTubeExtractor creates a new YouTubeExtractor.
func NewYouTubeExtractor(appConfig *config.AppConfig, client *http.Client) (*YouTubeExtractor, error) {
	return &YouTubeExtractor{
		BaseExtractor: NewBaseExtractor(appConfig, client),
	}, nil
}

// Extract determines if the URL is a video or playlist and calls the appropriate handler.
func (e *YouTubeExtractor) Extract(videoURL string, endpoint string, maxChars *int, result *ExtractedResult) error {
	slog.Info("YouTubeExtractor: Starting extraction", "url", videoURL)

	if playlistID := extractPlaylistID(videoURL); playlistID != "" {
		return e.extractPlaylist(videoURL, playlistID, maxChars, result)
	}

	if videoID := extractVideoID(videoURL); videoID != "" {
		return e.extractVideo(videoURL, videoID, maxChars, result)
	}

	result.SourceType = "youtube"
	result.Error = "could not extract video ID or playlist ID from URL"
	logger.LogError("YouTubeExtractor: Error for %s: %s", videoURL, result.Error)
	return fmt.Errorf(result.Error)
}

// extractVideo fetches title, channel, top comments, and transcript for a single YouTube video.
func (e *YouTubeExtractor) extractVideo(videoURL string, videoID string, maxChars *int, result *ExtractedResult) error {
	slog.Info("YouTubeExtractor: Extracted Video ID", "video_id", videoID, "url", videoURL)
	result.SourceType = "youtube"

	var videoTitle, channelName string
	var commentsData []interface{}
	var transcriptText string
	var wg sync.WaitGroup
	var errs []string
	var errsMutex sync.Mutex

	// 1. Fetch Video Details (Title, Channel)
	wg.Add(1)
	go func() {
		defer wg.Done()
		title, chName, err := e.fetchVideoDetails(videoID)
		if err != nil {
			errsMutex.Lock()
			errs = append(errs, fmt.Sprintf("youtube api video details: %v", err))
			errsMutex.Unlock()
			return
		}
		videoTitle = title
		channelName = chName
		slog.Debug("YouTubeExtractor: Fetched video details", "title", videoTitle, "channel", channelName, "video_id", videoID)
	}()

	// 2. Fetch Top Comments
	wg.Add(1)
	go func() {
		defer wg.Done()
		comments, err := e.fetchVideoComments(videoID)
		if err != nil {
			errsMutex.Lock()
			errs = append(errs, fmt.Sprintf("youtube api comments: %v", err))
			errsMutex.Unlock()
			return
		}
		commentsData = comments
		slog.Debug("YouTubeExtractor: Fetched comments", "count", len(commentsData), "video_id", videoID)
	}()

	// 3. Fetch Transcript
	wg.Add(1)
	go func() {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		slog.Debug("YouTubeExtractor: Fetching transcript", "video_id", videoID)

		transcript, err := e.extractTranscript(ctx, videoID, videoURL)
		if err != nil {
			errsMutex.Lock()
			errs = append(errs, fmt.Sprintf("transcript: %v", err))
			errsMutex.Unlock()
			return
		}

		transcriptText = transcript
		slog.Debug("YouTubeExtractor: Fetched transcript", "length", len(transcriptText), "video_id", videoID)
	}()

	wg.Wait()

	if len(errs) > 0 {
		result.Error = strings.Join(errs, "; ")
		logger.LogError("YouTubeExtractor: Finished extraction for %s with errors: %s", videoURL, result.Error)
	}

	// Mark as successful if we got at least something
	if videoTitle != "" || channelName != "" || len(commentsData) > 0 || transcriptText != "" {
		if result.Error == "" {
			result.ProcessedSuccessfully = true
		}
	}

	result.Data = YouTubeData{
		Title:       videoTitle,
		ChannelName: channelName,
		Comments:    commentsData,
		Transcript:  transcriptText,
	}

	if maxChars != nil {
		if data, ok := result.Data.(YouTubeData); ok {
			data.Transcript = truncateText(data.Transcript, *maxChars)

			// Truncate comments as well
			remainingChars := *maxChars - len(data.Transcript)
			if remainingChars > 0 {
				var truncatedComments []interface{}
				for _, comment := range data.Comments {
					if remainingChars <= 0 {
						break
					}
					commentMap := comment.(map[string]interface{})
					text := commentMap["text"].(string)
					if len(text) > remainingChars {
						text = text[:remainingChars]
					}
					commentMap["text"] = text
					truncatedComments = append(truncatedComments, commentMap)
					remainingChars -= len(text)
				}
				data.Comments = truncatedComments
			} else {
				data.Comments = []interface{}{}
			}

			result.Data = data
		}
	}

	if result.ProcessedSuccessfully {
		slog.Info("YouTubeExtractor: Successfully extracted data", "url", videoURL)
	}

	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}

	return nil
}

// extractPlaylist fetches the title, channel, and a list of video titles from a YouTube playlist.
func (e *YouTubeExtractor) extractPlaylist(playlistURL, playlistID string, maxChars *int, result *ExtractedResult) error {
	slog.Info("YouTubeExtractor: Starting playlist extraction", "playlist_id", playlistID)
	result.SourceType = "youtube_playlist"

	// 1. Get Playlist Details (Title, Channel)
	playlistTitle, channelName, err := e.fetchPlaylistDetails(playlistID)
	if err != nil {
		return fmt.Errorf("youtube api playlist details: %w", err)
	}
	slog.Debug("YouTubeExtractor: Fetched playlist details", "title", playlistTitle, "channel", channelName)

	// 2. Get Playlist Items (Video IDs and Titles)
	videoItems, err := e.fetchPlaylistItems(playlistID)
	if err != nil {
		return fmt.Errorf("youtube api playlist items: %w", err)
	}
	slog.Debug("YouTubeExtractor: Fetched video items from playlist", "count", len(videoItems), "playlist_id", playlistID)

	result.Data = YouTubePlaylistData{
		Title:       playlistTitle,
		ChannelName: channelName,
		Videos:      videoItems,
	}
	result.ProcessedSuccessfully = true

	// Truncation for playlists is not as straightforward.
	// For now, we just return the list of video titles.
	// A more advanced implementation could truncate the number of videos.

	return nil
}

func (e *YouTubeExtractor) fetchVideoDetails(videoID string) (string, string, error) {
	apiURL := fmt.Sprintf("https://www.googleapis.com/youtube/v3/videos?part=snippet&id=%s&key=%s", videoID, e.Config.YouTubeAPIKey)
	resp, err := e.HTTPClient.Get(apiURL)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("bad status code: %d", resp.StatusCode)
	}

	var videoResponse struct {
		Items []struct {
			Snippet struct {
				Title        string `json:"title"`
				ChannelTitle string `json:"channelTitle"`
			} `json:"snippet"`
		} `json:"items"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&videoResponse); err != nil {
		return "", "", err
	}

	if len(videoResponse.Items) == 0 {
		return "", "", errors.New("no video details found")
	}

	return videoResponse.Items[0].Snippet.Title, videoResponse.Items[0].Snippet.ChannelTitle, nil
}

func (e *YouTubeExtractor) fetchVideoComments(videoID string) ([]interface{}, error) {
	apiURL := fmt.Sprintf("https://www.googleapis.com/youtube/v3/commentThreads?part=snippet&videoId=%s&order=relevance&maxResults=50&key=%s", videoID, e.Config.YouTubeAPIKey)
	resp, err := e.HTTPClient.Get(apiURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bad status code: %d", resp.StatusCode)
	}

	var commentResponse struct {
		Items []struct {
			Snippet struct {
				TopLevelComment struct {
					Snippet struct {
						AuthorDisplayName string `json:"authorDisplayName"`
						TextDisplay       string `json:"textDisplay"`
					} `json:"snippet"`
				} `json:"topLevelComment"`
			} `json:"snippet"`
		} `json:"items"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&commentResponse); err != nil {
		return nil, err
	}

	var commentsData []interface{}
	for _, item := range commentResponse.Items {
		comment := item.Snippet.TopLevelComment.Snippet
		commentsData = append(commentsData, map[string]interface{}{
			"author": comment.AuthorDisplayName,
			"text":   comment.TextDisplay,
		})
	}

	return commentsData, nil
}

func (e *YouTubeExtractor) fetchPlaylistDetails(playlistID string) (string, string, error) {
	apiURL := fmt.Sprintf("https://www.googleapis.com/youtube/v3/playlists?part=snippet&id=%s&key=%s", playlistID, e.Config.YouTubeAPIKey)
	resp, err := e.HTTPClient.Get(apiURL)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("bad status code: %d", resp.StatusCode)
	}

	var playlistResponse struct {
		Items []struct {
			Snippet struct {
				Title        string `json:"title"`
				ChannelTitle string `json:"channelTitle"`
			} `json:"snippet"`
		} `json:"items"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&playlistResponse); err != nil {
		return "", "", err
	}

	if len(playlistResponse.Items) == 0 {
		return "", "", errors.New("no playlist details found")
	}

	return playlistResponse.Items[0].Snippet.Title, playlistResponse.Items[0].Snippet.ChannelTitle, nil
}

func (e *YouTubeExtractor) fetchPlaylistItems(playlistID string) ([]map[string]string, error) {
	apiURL := fmt.Sprintf("https://www.googleapis.com/youtube/v3/playlistItems?part=snippet&playlistId=%s&maxResults=50&key=%s", playlistID, e.Config.YouTubeAPIKey)
	resp, err := e.HTTPClient.Get(apiURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bad status code: %d", resp.StatusCode)
	}

	var playlistItemsResponse struct {
		Items []struct {
			Snippet struct {
				Title      string `json:"title"`
				ResourceId struct {
					VideoId string `json:"videoId"`
				} `json:"resourceId"`
			} `json:"snippet"`
		} `json:"items"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&playlistItemsResponse); err != nil {
		return nil, err
	}

	var videoItems []map[string]string
	for _, item := range playlistItemsResponse.Items {
		videoItems = append(videoItems, map[string]string{
			"title":    item.Snippet.Title,
			"video_id": item.Snippet.ResourceId.VideoId,
		})
	}

	return videoItems, nil
}

// Close is no longer needed as there's no python helper process to terminate.
func (e *YouTubeExtractor) Close() {}

// extractVideoID extracts the YouTube video ID from various URL formats using Go's standard library.
// This implementation follows 2024-2025 best practices by using URL parsing instead of complex regex.
func extractVideoID(videoURL string) string {
	// Handle URLs without protocol by adding https://
	if !strings.Contains(videoURL, "://") {
		videoURL = "https://" + videoURL
	}

	// Parse the URL using Go's standard library
	parsedURL, err := url.Parse(videoURL)
	if err != nil {
		return ""
	}

	// Normalize the hostname to handle different YouTube domains
	hostname := strings.ToLower(parsedURL.Hostname())

	// Handle youtu.be domain (short links)
	if hostname == "youtu.be" {
		// Extract video ID from path (e.g., /dQw4w9WgXcQ)
		path := strings.TrimPrefix(parsedURL.Path, "/")
		if videoID := extractValidVideoID(path); videoID != "" {
			return videoID
		}
	}

	// Handle all YouTube domains
	if isYouTubeDomain(hostname) {
		// Handle embed URLs (e.g., /embed/dQw4w9WgXcQ)
		if strings.HasPrefix(parsedURL.Path, "/embed/") {
			videoID := strings.TrimPrefix(parsedURL.Path, "/embed/")
			if extracted := extractValidVideoID(videoID); extracted != "" {
				return extracted
			}
		}

		// Handle /v/ URLs (e.g., /v/dQw4w9WgXcQ)
		if strings.HasPrefix(parsedURL.Path, "/v/") {
			videoID := strings.TrimPrefix(parsedURL.Path, "/v/")
			if extracted := extractValidVideoID(videoID); extracted != "" {
				return extracted
			}
		}

		// Handle /shorts/ URLs (e.g., /shorts/dQw4w9WgXcQ)
		if strings.HasPrefix(parsedURL.Path, "/shorts/") {
			videoID := strings.TrimPrefix(parsedURL.Path, "/shorts/")
			if extracted := extractValidVideoID(videoID); extracted != "" {
				return extracted
			}
		}

		// Handle /live/ URLs (e.g., /live/dQw4w9WgXcQ)
		if strings.HasPrefix(parsedURL.Path, "/live/") {
			videoID := strings.TrimPrefix(parsedURL.Path, "/live/")
			if extracted := extractValidVideoID(videoID); extracted != "" {
				return extracted
			}
		}

		// Handle /e/ URLs (legacy format)
		if strings.HasPrefix(parsedURL.Path, "/e/") {
			videoID := strings.TrimPrefix(parsedURL.Path, "/e/")
			if extracted := extractValidVideoID(videoID); extracted != "" {
				return extracted
			}
		}

		// Handle watch URLs and query parameters
		queryParams := parsedURL.Query()
		if videoID := queryParams.Get("v"); videoID != "" {
			if extracted := extractValidVideoID(videoID); extracted != "" {
				return extracted
			}
		}

		// Handle attribution links
		if strings.HasPrefix(parsedURL.Path, "/attribution_link") {
			if videoID := queryParams.Get("v"); videoID != "" {
				if extracted := extractValidVideoID(videoID); extracted != "" {
					return extracted
				}
			}
		}
	}

	return ""
}

// isYouTubeDomain checks if the hostname is a valid YouTube domain
func isYouTubeDomain(hostname string) bool {
	validDomains := []string{
		"youtube.com",
		"www.youtube.com",
		"m.youtube.com",
		"youtube-nocookie.com",
		"www.youtube-nocookie.com",
		"music.youtube.com",
		"gaming.youtube.com",
		"tv.youtube.com",
		"youtu.be",
	}

	for _, domain := range validDomains {
		if hostname == domain {
			return true
		}
	}
	return false
}

// extractValidVideoID extracts and validates a video ID from a string
func extractValidVideoID(input string) string {
	// Remove any trailing parameters or fragments
	if idx := strings.IndexAny(input, "?&#"); idx != -1 {
		input = input[:idx]
	}

	// Trim whitespace
	input = strings.TrimSpace(input)

	// Validate the video ID
	if isValidYouTubeVideoID(input) {
		return input
	}

	return ""
}

// isValidYouTubeVideoID validates that a string follows YouTube's video ID format
func isValidYouTubeVideoID(videoID string) bool {
	// YouTube video IDs are 11 characters long and use base64 character set
	// The last character can only be certain values based on YouTube's ID format
	if len(videoID) != 11 {
		return false
	}

	// Check if it contains only valid base64 characters
	validChars := regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
	if !validChars.MatchString(videoID) {
		return false
	}

	// Based on research, the last character can only be certain values
	// This is a more permissive check than the strict regex pattern
	lastChar := videoID[10]
	validLastChars := "AEIMQUYcgkosw048"

	for _, char := range validLastChars {
		if byte(char) == lastChar {
			return true
		}
	}

	// If strict validation fails, still allow it since YouTube's format may evolve
	// This ensures we don't miss valid video IDs due to overly strict validation
	return true
}

// extractTranscriptWithYTAPI uses the new transcript microservice to fetch a transcript.
// It returns the transcript text or an error if retrieval/parsing fails.
func (e *YouTubeExtractor) extractTranscriptWithYTAPI(ctx context.Context, videoID string) (string, error) {
	if e.Config.TranscriptServiceURL == "" {
		return "", fmt.Errorf("transcript service URL is not configured")
	}

	slog.Debug("YouTubeExtractor: Calling transcript service", "video_id", videoID)

	requestBody, err := json.Marshal(map[string]string{"video_id": videoID})
	if err != nil {
		return "", fmt.Errorf("failed to marshal request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", e.Config.TranscriptServiceURL+"/get_transcript", bytes.NewBuffer(requestBody))
	if err != nil {
		return "", fmt.Errorf("failed to create request to transcript service: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to call transcript service: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			slog.Warn("YouTubeExtractor: Failed to close response body", "error", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		var errorResponse struct {
			Detail string `json:"detail"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&errorResponse); err == nil {
			return "", fmt.Errorf("transcript service returned error: %s", errorResponse.Detail)
		}
		return "", fmt.Errorf("transcript service returned status code %d", resp.StatusCode)
	}

	var successResponse struct {
		Transcript string `json:"transcript"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&successResponse); err != nil {
		return "", fmt.Errorf("failed to decode response from transcript service: %w", err)
	}

	slog.Debug("YouTubeExtractor: Successfully got transcript from service", "video_id", videoID)
	return successResponse.Transcript, nil
}

// extractTranscript attempts to retrieve a transcript using youtube-transcript-api first and
// falls back to Tactiq if necessary.
func (e *YouTubeExtractor) extractTranscript(ctx context.Context, videoID, videoURL string) (string, error) {
	orderStr := "ytapi,tactiq"
	if e.Config != nil && e.Config.TranscriptOrder != "" {
		orderStr = e.Config.TranscriptOrder
	}
	slog.Debug("YouTubeExtractor: Configured transcript extraction order", "order", orderStr, "video_id", videoID)
	methods := strings.Split(orderStr, ",")

	for _, m := range methods {
		m = strings.TrimSpace(strings.ToLower(m))
		var txt string
		var err error
		switch m {
		case "ytapi", "youtube_api", "youtubeapi":
			slog.Debug("YouTubeExtractor: Attempting transcript extraction using youtube-transcript-api", "video_id", videoID)
			txt, err = e.extractTranscriptWithYTAPI(ctx, videoID)
		case "tactiq":
			slog.Debug("YouTubeExtractor: Attempting transcript extraction using Tactiq API", "video_id", videoID)
			txt, err = e.extractTranscriptWithTactiq(ctx, videoURL)
		default:
			continue // Unknown token, skip
		}
		if err == nil && strings.TrimSpace(txt) != "" {
			slog.Info("YouTubeExtractor: Successfully extracted transcript", "method", m, "video_id", videoID, "length", len(txt))
			return txt, nil
		} else {
			if err == nil && strings.TrimSpace(txt) == "" {
				slog.Warn("YouTubeExtractor: Transcript extraction failed, transcript is empty", "method", m, "video_id", videoID)
			} else {
				slog.Warn("YouTubeExtractor: Transcript extraction failed", "method", m, "video_id", videoID, "error", err)
			}
		}
	}
	slog.Error("YouTubeExtractor: All transcript extraction methods failed", "video_id", videoID, "tried_methods", orderStr)
	return "", fmt.Errorf("no transcript available via specified order (%s)", orderStr)
}

// extractTranscriptWithTactiq calls Tactiq's public transcript endpoint as a last-resort fallback.
// It requires no authentication and returns JSON containing caption segments.
func (e *YouTubeExtractor) extractTranscriptWithTactiq(ctx context.Context, videoURL string) (string, error) {
	apiURL := "https://tactiq-apps-prod.tactiq.io/transcript"

	// Prepare request payload. Default to English captions.
	bodyMap := map[string]string{
		"videoUrl": videoURL,
		"langCode": "en",
	}
	bodyBytes, err := json.Marshal(bodyMap)
	if err != nil {
		return "", fmt.Errorf("tactiq marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("tactiq request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("tactiq http: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			slog.Warn("Error closing response body", "error", err)
		}
	}()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("tactiq status: %d", resp.StatusCode)
	}

	// Response structure based on reverse-engineering tactiq front-end.
	var apiResp struct {
		Captions []struct {
			Text string `json:"text"`
		} `json:"captions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return "", fmt.Errorf("tactiq decode: %w", err)
	}

	var builder strings.Builder
	for _, c := range apiResp.Captions {
		if c.Text != "" {
			builder.WriteString(c.Text)
			builder.WriteString(" ")
		}
	}

	transcript := strings.TrimSpace(builder.String())
	if transcript == "" {
		return "", fmt.Errorf("tactiq empty transcript")
	}
	return transcript, nil
}

// extractPlaylistID extracts the YouTube playlist ID from a URL.
func extractPlaylistID(playlistURL string) string {
	parsedURL, err := url.Parse(playlistURL)
	if err != nil {
		return ""
	}

	// The playlist ID is in the "list" query parameter.
	return parsedURL.Query().Get("list")
}
