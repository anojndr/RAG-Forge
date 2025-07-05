package extractor

import (
	"context"
	"log"
)

func DebugExtract() {
	yt := &YouTubeExtractor{}
	txt, err := yt.extractTranscript(context.Background(), "RgBYohJ7mIk", "https://www.youtube.com/watch?v=RgBYohJ7mIk")
	log.Printf("err=%v len=%d text-start=%q", err, len(txt), txt[:min(100, len(txt))])
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
