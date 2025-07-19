package extractor

import (
	"log"
	"net/http"
	"web-search-api-for-llms/internal/config"
)

func DebugExtract() {
	appConfig, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	client := &http.Client{}
	yt, err := NewYouTubeExtractor(appConfig, client)
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

