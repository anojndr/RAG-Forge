package extractor

import (
	"log"
	"net/http"
	"web-search-api-for-llms/internal/config"
	"web-search-api-for-llms/internal/utils"
)

func DebugExtract() {
	appConfig, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	client := &http.Client{}
	pool, err := utils.NewPythonPool(5, func() (*utils.PythonHelper, error) {
		return utils.NewPythonHelper("internal/extractor/youtube_helper.py")
	})
	if err != nil {
		log.Fatalf("Failed to create python pool: %v", err)
	}
	defer pool.Close()

	yt, err := NewYouTubeExtractor(appConfig, client, pool)
	if err != nil {
		log.Fatalf("Failed to create YouTubeExtractor: %v", err)
	}

	result, err := yt.Extract("https://www.youtube.com/watch?v=RgBYohJ7mIk", nil)
	if err != nil {
		log.Printf("Error extracting: %v", err)
	}
	log.Printf("Result: %+v", result)
}

