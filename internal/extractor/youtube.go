package extractor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"google.golang.org/api/option"
	"google.golang.org/api/youtube/v3"

	"web-search-api-for-llms/internal/config"
	"web-search-api-for-llms/internal/logger"
	"web-search-api-for-llms/internal/utils"
)

// YouTubeExtractor implements the Extractor interface for YouTube URLs.
type YouTubeExtractor struct {
	BaseExtractor
	youtubeService *youtube.Service
}

// NewYouTubeExtractor creates a new YouTubeExtractor.
func NewYouTubeExtractor(appConfig *config.AppConfig) (*YouTubeExtractor, error) {
	ctx := context.Background()
	ytService, err := youtube.NewService(ctx, option.WithAPIKey(appConfig.YouTubeAPIKey))
	if err != nil {
		return nil, fmt.Errorf("failed to create YouTube service: %w", err)
	}

	return &YouTubeExtractor{
		BaseExtractor:  BaseExtractor{Config: appConfig},
		youtubeService: ytService,
	}, nil
}

// Extract fetches title, channel, top comments, and transcript for a YouTube video.
func (e *YouTubeExtractor) Extract(videoURL string) (*ExtractedResult, error) {
	log.Printf("YouTubeExtractor: Starting extraction for URL: %s", videoURL)
	result := &ExtractedResult{
		URL:        videoURL,
		SourceType: "youtube",
	}

	videoID := extractVideoID(videoURL)
	if videoID == "" {
		result.Error = "could not extract video ID from URL"
		logger.LogError("YouTubeExtractor: Error for %s: %s", videoURL, result.Error)
		return result, fmt.Errorf(result.Error)
	}

	log.Printf("YouTubeExtractor: Extracted Video ID: %s for URL: %s", videoID, videoURL)

	var videoTitle, channelName string
	var commentsData []interface{}
	var transcriptText string
	var wg sync.WaitGroup
	var errs []string

	// 1. Fetch Video Details (Title, Channel)
	wg.Add(1)
	go func() {
		defer wg.Done()
		call := e.youtubeService.Videos.List([]string{"snippet"}).Id(videoID)
		resp, err := call.Do()
		if err != nil {
			logger.LogError("YouTubeExtractor: Error fetching video details for %s: %v", videoID, err)
			errs = append(errs, fmt.Sprintf("youtube api video details: %v", err))
			return
		}
		if len(resp.Items) > 0 {
			videoTitle = resp.Items[0].Snippet.Title
			channelName = resp.Items[0].Snippet.ChannelTitle
			log.Printf("YouTubeExtractor: Fetched Title: '%s', Channel: '%s' for %s", videoTitle, channelName, videoID)
		} else {
			log.Printf("YouTubeExtractor: No video details found for %s", videoID)
			errs = append(errs, "youtube api: no video details found")
		}
	}()

	// 2. Fetch Top Comments
	wg.Add(1)
	go func() {
		defer wg.Done()
		call := e.youtubeService.CommentThreads.List([]string{"snippet"}).
			VideoId(videoID).
			Order("relevance").
			MaxResults(50)

		resp, err := call.Do()
		if err != nil {
			logger.LogError("YouTubeExtractor: Error fetching comments for %s: %v", videoID, err)
			errs = append(errs, fmt.Sprintf("youtube api comments: %v", err))
			return
		}
		for _, item := range resp.Items {
			comment := item.Snippet.TopLevelComment.Snippet
			commentsData = append(commentsData, map[string]interface{}{
				"author": comment.AuthorDisplayName,
				"text":   comment.TextDisplay,
			})
		}
		log.Printf("YouTubeExtractor: Fetched %d comments for %s", len(commentsData), videoID)
	}()

	// 3. Fetch Transcript using yt-dlp command line
	wg.Add(1)
	go func() {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		log.Printf("YouTubeExtractor: Fetching transcript for %s", videoID)

		transcript, err := e.extractTranscript(ctx, videoID, videoURL)
		if err != nil {
			logger.LogError("YouTubeExtractor: Error extracting transcript for %s: %v", videoID, err)
			errs = append(errs, fmt.Sprintf("transcript: %v", err))
			return
		}

		transcriptText = transcript
		log.Printf("YouTubeExtractor: Fetched transcript (length %d) for %s", len(transcriptText), videoID)
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

	if result.ProcessedSuccessfully {
		log.Printf("YouTubeExtractor: Successfully extracted data for %s", videoURL)
	}

	return result, nil
}

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

// extractTranscriptWithYTAPI uses the python youtube-transcript-api package to fetch a transcript.
// It returns the transcript text or an error if retrieval/parsing fails.
func (e *YouTubeExtractor) extractTranscriptWithYTAPI(ctx context.Context, videoID string) (string, error) {
	// Build the python script which conditionally enables Webshare proxy support.
	pythonLines := []string{
		"import os, sys, json, subprocess, importlib.util",
		"",
		"# Ensure youtube_transcript_api is available. Install silently if missing.",
		"package_name = \"youtube_transcript_api\"",
		"if importlib.util.find_spec(package_name) is None:",
		"    # Use cross-platform package installation",
		"    try:",
		"        subprocess.check_call([sys.executable, '-m', 'pip', 'install', '--quiet', package_name])",
		"    except subprocess.CalledProcessError:",
		"        # Fallback for systems requiring --break-system-packages",
		"        subprocess.check_call([sys.executable, '-m', 'pip', 'install', '--quiet', '--break-system-packages', package_name])",
		"",
		"from youtube_transcript_api import YouTubeTranscriptApi",
		"from youtube_transcript_api._errors import TranscriptsDisabled, NoTranscriptFound, VideoUnavailable",
		"",
		"# Check for Webshare credentials via environment variables",
		"username = os.getenv('WEBSHARE_PROXY_USERNAME')",
		"password = os.getenv('WEBSHARE_PROXY_PASSWORD')",
		"",
		"try:",
		"    if username and password:",
		"        from youtube_transcript_api.proxies import WebshareProxyConfig",
		"        api = YouTubeTranscriptApi(proxy_config=WebshareProxyConfig(proxy_username=username, proxy_password=password))",
		"    else:",
		"        api = YouTubeTranscriptApi()",
		"    ",
		"    video_id = sys.argv[1]",
		"    ",
		"    # Try to get transcript with multiple language preferences",
		"    try:",
		"        transcript = api.get_transcript(video_id, languages=['en', 'en-US', 'en-GB'])",
		"    except NoTranscriptFound:",
		"        # If English not available, try to get any available transcript",
		"        transcript_list = api.list_transcripts(video_id)",
		"        transcript = transcript_list.find_transcript(['en', 'en-US', 'en-GB']).fetch()",
		"    ",
		"    print(json.dumps(transcript))",
		"    ",
		"except TranscriptsDisabled:",
		"    print(json.dumps({'error': 'Transcripts are disabled for this video'}))",
		"except NoTranscriptFound:",
		"    print(json.dumps({'error': 'No transcript found for this video'}))",
		"except VideoUnavailable:",
		"    print(json.dumps({'error': 'Video is unavailable'}))",
		"except Exception as e:",
		"    print(json.dumps({'error': f'Failed to extract transcript: {str(e)}'}))",
	}

	pythonScript := strings.Join(pythonLines, "\n")

	// Ensure venv exists before using it
	if err := utils.EnsureVenvExists(); err != nil {
		return "", fmt.Errorf("failed to ensure venv exists: %w", err)
	}

	// Use venv Python only
	pythonCmd := "./venv/bin/python"
	if runtime.GOOS == "windows" {
		pythonCmd = "./venv/Scripts/python.exe"
	}

	cmd := exec.CommandContext(ctx, pythonCmd, "-c", pythonScript, videoID)

	// Pass Webshare credentials to the python process via environment variables if configured
	if e.Config != nil && e.Config.WebshareProxyUsername != "" && e.Config.WebshareProxyPassword != "" {
		cmd.Env = append(cmd.Env, fmt.Sprintf("WEBSHARE_PROXY_USERNAME=%s", e.Config.WebshareProxyUsername))
		cmd.Env = append(cmd.Env, fmt.Sprintf("WEBSHARE_PROXY_PASSWORD=%s", e.Config.WebshareProxyPassword))
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("youtube_transcript_api command failed: %v; output: %s", err, string(output))
	}

	// First check if the response contains an error
	var errorResp struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(output, &errorResp); err == nil && errorResp.Error != "" {
		return "", fmt.Errorf("youtube_transcript_api error: %s", errorResp.Error)
	}

	// Try to parse as transcript segments
	var segments []struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(output, &segments); err != nil {
		return "", fmt.Errorf("failed to parse transcript json: %w; output: %s", err, string(output))
	}

	var builder strings.Builder
	for _, seg := range segments {
		if seg.Text != "" {
			builder.WriteString(seg.Text)
			builder.WriteString(" ")
		}
	}

	transcript := strings.TrimSpace(builder.String())
	if transcript == "" {
		return "", fmt.Errorf("empty transcript data")
	}
	return transcript, nil
}

// extractTranscript attempts to retrieve a transcript using youtube-transcript-api first and
// falls back to Tactiq if necessary.
func (e *YouTubeExtractor) extractTranscript(ctx context.Context, videoID, videoURL string) (string, error) {
	orderStr := "ytapi,tactiq"
	if e.Config != nil && e.Config.TranscriptOrder != "" {
		orderStr = e.Config.TranscriptOrder
	}
	log.Printf("YouTubeExtractor: Configured transcript extraction order: %s for %s", orderStr, videoID)
	methods := strings.Split(orderStr, ",")

	for _, m := range methods {
		m = strings.TrimSpace(strings.ToLower(m))
		var txt string
		var err error
		switch m {
		case "ytapi", "youtube_api", "youtubeapi":
			log.Printf("YouTubeExtractor: Attempting transcript extraction using youtube-transcript-api for %s", videoID)
			txt, err = e.extractTranscriptWithYTAPI(ctx, videoID)
		case "tactiq":
			log.Printf("YouTubeExtractor: Attempting transcript extraction using Tactiq API for %s", videoID)
			txt, err = e.extractTranscriptWithTactiq(ctx, videoURL)
		default:
			continue // Unknown token, skip
		}
		if err == nil && strings.TrimSpace(txt) != "" {
			log.Printf("YouTubeExtractor: Successfully extracted transcript using %s method for %s (length: %d)", m, videoID, len(txt))
			return txt, nil
		} else {
			if err == nil && strings.TrimSpace(txt) == "" {
				log.Printf("YouTubeExtractor: Failed to extract transcript using %s method for %s: transcript is empty", m, videoID)
			} else {
				log.Printf("YouTubeExtractor: Failed to extract transcript using %s method for %s: %v", m, videoID, err)
			}
		}
	}
	log.Printf("YouTubeExtractor: All transcript extraction methods failed for %s (tried: %s)", videoID, orderStr)
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

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("tactiq http: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Printf("Error closing response body: %v", err)
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
