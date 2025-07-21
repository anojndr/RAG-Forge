package main

import (
	"compress/gzip"
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"web-search-api-for-llms/internal/api"
	"web-search-api-for-llms/internal/browser"
	"web-search-api-for-llms/internal/cache"
	"web-search-api-for-llms/internal/config"
	"web-search-api-for-llms/internal/logger"
	"web-search-api-for-llms/internal/utils"
)

var gzipWriterPool = sync.Pool{
	New: func() interface{} {
		return gzip.NewWriter(nil)
	},
}

func main() {
	// Setup logging
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	// Load configuration
	appConfig, err := config.LoadConfig()
	if err != nil {
		logger.LogError("Failed to load configuration: %v", err)
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Validate system dependencies
	log.Println("Validating system dependencies...")
	if err := utils.ValidateSystemDependencies(); err != nil {
		logger.LogError("System dependency validation failed: %v", err)
		log.Printf("Warning: System dependency validation failed: %v", err)
		log.Printf("Some features may not work correctly. Please ensure:")
		log.Printf("  - Python 3.8+ is installed and available")
		log.Printf("  - pip is available with Python")
		log.Printf("  - pdftotext is installed (poppler-utils package)")
		log.Printf("Continuing startup...")
	} else {
		log.Printf("System dependencies validated successfully (Python: %s)", utils.GetPythonCommand())
	}

	// Initialize browser pool
	browserPool, err := browser.NewPool(appConfig.BrowserPoolSize)
	if err != nil {
		log.Fatalf("Failed to create browser pool: %v", err)
	}
	defer browserPool.Cleanup()

	// Create a single, optimized HTTP client for all network requests
	httpClient := &http.Client{
		Timeout: 30 * time.Second, // Generous timeout for extractors
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
			ForceAttemptHTTP2:   true,
		},
	}

	// Initialize cache based on configuration
	var appCache cache.Cache
	switch appConfig.CacheType {
	case "redis":
		log.Println("Using Redis cache")
		appCache = cache.NewRedisCache(appConfig.RedisURL, appConfig.RedisPassword, appConfig.RedisDB)
	default:
		log.Println("Using in-memory cache")
		appCache = cache.NewMemoryCache(10*time.Minute, 15*time.Minute)
	}

	// Ensure Python dependencies are installed in venv
	log.Println("Ensuring Python dependencies are installed in venv...")
	if err := utils.InstallPythonPackage("youtube-transcript-api"); err != nil {
		// Log a warning but don't fail, as it might already be installed
		logger.LogErrorf("Warning: could not ensure python package is installed: %v", err)
	} else {
		log.Println("Python dependencies verified.")
	}

	// Initialize Python helper pool
	pythonPool, err := utils.NewPythonPool(appConfig.PythonPoolSize, func() (*utils.PythonHelper, error) {
		return utils.NewPythonHelper("internal/extractor/youtube_helper.py")
	})
	if err != nil {
		log.Fatalf("Failed to create python pool: %v", err)
	}
	defer pythonPool.Close()

	// Initialize handlers
	searchHandler := api.NewSearchHandler(appConfig, browserPool, pythonPool, httpClient, appCache)

	// Setup HTTP server
	mux := http.NewServeMux()
	mux.HandleFunc("/search", searchHandler.HandleSearch)
	mux.HandleFunc("/extract", searchHandler.HandleExtract)

	// Add health check endpoint
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if _, err := fmt.Fprintf(w, `{"status":"healthy","timestamp":"%s"}`, time.Now().Format(time.RFC3339)); err != nil {
			logger.LogError("Warning: failed to write health check response: %v", err)
		}
	})

	// Create compression and timeout middleware
	handler := gzipMiddleware(timeoutMiddleware(mux))

	server := &http.Server{
		Addr:         ":8086",
		Handler:      handler,
		ReadTimeout:  60 * time.Second,  // Increased from 30s
		WriteTimeout: 120 * time.Second, // Increased from 30s for Twitter extraction
		IdleTimeout:  120 * time.Second, // Increased from 60s
	}

	// Start server in a goroutine
	go func() {
		log.Printf("Starting server on port 8086")
		log.Printf("Available endpoints:")
		log.Printf("  POST /search  - Search and extract content from search results")
		log.Printf("  POST /extract - Extract content from provided URLs")
		log.Printf("  GET  /health  - Health check endpoint")

		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.LogError("Server failed to start: %v", err)
			log.Fatalf("Server failed to start: %v", err)
		}
	}()

	// Setup graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	// Wait for interrupt signal
	<-quit
	log.Println("Shutting down server...")

	// Create shutdown context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Shutdown server gracefully
	if err := server.Shutdown(ctx); err != nil {
		logger.LogError("Server forced to shutdown: %v", err)
		os.Exit(1)
	}

	log.Println("Server exited gracefully")
}

// gzipMiddleware compresses responses when the client supports it
func gzipMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if client supports gzip
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}

		// Set gzip headers
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Vary", "Accept-Encoding")

		// Get a gzip writer from the pool
		gw := gzipWriterPool.Get().(*gzip.Writer)
		gw.Reset(w)
		defer func() {
			if err := gw.Close(); err != nil {
				logger.LogError("Error closing gzip writer: %v", err)
			}
			gzipWriterPool.Put(gw)
		}()

		// Wrap response writer
		grw := &gzipResponseWriter{ResponseWriter: w, writer: gw}
		next.ServeHTTP(grw, r)
	})
}

// gzipResponseWriter wraps http.ResponseWriter to compress responses
type gzipResponseWriter struct {
	http.ResponseWriter
	writer *gzip.Writer
}

func (w *gzipResponseWriter) Write(b []byte) (int, error) {
	return w.writer.Write(b)
}

func (w *gzipResponseWriter) Header() http.Header {
	return w.ResponseWriter.Header()
}

// timeoutMiddleware adds request timeout handling
func timeoutMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Set a reasonable timeout for requests
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute) // 3 minutes for Twitter extraction
		defer cancel()

		// Create new request with timeout context
		r = r.WithContext(ctx)

		// Handle timeout
		done := make(chan struct{})
		go func() {
			next.ServeHTTP(w, r)
			close(done)
		}()

		select {
		case <-done:
			// Request completed successfully
			return
		case <-ctx.Done():
			// Request timed out
			logger.LogError("Request timed out: %s %s", r.Method, r.URL.Path)
			http.Error(w, "Request timeout", http.StatusGatewayTimeout)
			return
		}
	})
}
