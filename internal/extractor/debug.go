package extractor

import (
	"log"
	"web-search-api-for-llms/internal/config"
)

func DebugExtract() {
	appConfig, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	yt, err := NewYouTubeExtractor(appConfig)
	if err != nil {
		log.Fatalf("Failed to create YouTubeExtractor: %v", err)
	}
	defer yt.Close()

	result, err := yt.Extract("https://www.youtube.com/watch?v=RgBYohJ7mIk")
	if err != nil {
		log.Printf("Error extracting: %v", err)
	}
	log.Printf("Result: %+v", result)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
