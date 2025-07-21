package main

import (
	"compress/gzip"
	"context"
	"log/slog"
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
	"web-search-api-for-llms/internal/extractor"
	"web-search-api-for-llms/internal/worker"
)

var gzipWriterPool = sync.Pool{
	New: func() interface{} {
		return gzip.NewWriter(nil)
	},
}

func main() {
	// Setup high-performance structured logging
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	// Load configuration
	appConfig, err := config.LoadConfig()
	if err != nil {
		slog.Error("Failed to load configuration", "error", err)
		os.Exit(1)
	}

	// Initialize browser pool
	browserPool, err := browser.NewPool(appConfig.BrowserPoolSize)
	if err != nil {
		slog.Error("Failed to create browser pool", "error", err)
		os.Exit(1)
	}
	defer browserPool.Cleanup()

	// Create a single, optimized HTTP client for all network requests
	httpClient := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        200, // Increased for high concurrency
			MaxIdleConnsPerHost: 20,  // Increased for high concurrency
			IdleConnTimeout:     90 * time.Second,
			ForceAttemptHTTP2:   true,
		},
	}

	// Initialize cache based on configuration
	var appCache cache.Cache
	switch appConfig.CacheType {
	case "redis":
		slog.Info("Using Redis cache")
		appCache = cache.NewRedisCache(appConfig.RedisURL, appConfig.RedisPassword, appConfig.RedisDB)
	default:
		slog.Info("Using in-memory cache")
		appCache = cache.NewMemoryCache(10*time.Minute, 15*time.Minute)
	}

	// Create a single dispatcher instance
	dispatcher := extractor.NewDispatcher(appConfig, browserPool, httpClient)

	// === DUAL WORKER POOL INITIALIZATION ===
	// A small pool for heavy, browser-based jobs
	browserWorkerPool := worker.NewWorkerPool(dispatcher, appConfig.BrowserPoolSize, appConfig.BrowserPoolSize*2)
	browserWorkerPool.Start()
	defer browserWorkerPool.Stop()
	slog.Info("Browser worker pool started", "size", appConfig.BrowserPoolSize)

	// A large pool for light, HTTP-based jobs
	httpWorkerPool := worker.NewWorkerPool(dispatcher, appConfig.MaxConcurrentExtractions, 200)
	httpWorkerPool.Start()
	defer httpWorkerPool.Stop()
	slog.Info("HTTP worker pool started", "size", appConfig.MaxConcurrentExtractions)

	// Initialize handlers, passing the worker pools
	searchHandler := api.NewSearchHandler(appConfig, browserPool, httpClient, appCache, httpWorkerPool, browserWorkerPool)

	// Setup HTTP server
	mux := http.NewServeMux()
	mux.HandleFunc("/search", searchHandler.HandleSearch)
	mux.HandleFunc("/extract", searchHandler.HandleExtract)

	// Add health check endpoint
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// Use jsoniter for consistency and performance
		jsoniter := api.GetJsoniter()
		if err := jsoniter.NewEncoder(w).Encode(map[string]string{"status": "healthy", "timestamp": time.Now().Format(time.RFC3339)}); err != nil {
			slog.Warn("Failed to write health check response", "error", err)
		}
	})

	// Create compression and timeout middleware
	handler := gzipMiddleware(timeoutMiddleware(mux))

	server := &http.Server{
		Addr:         ":8086",
		Handler:      handler,
		ReadTimeout:  60 * time.Second,
		WriteTimeout: 120 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Start server in a goroutine
	go func() {
		slog.Info("Starting server", "port", 8086)
		slog.Info("Available endpoints", "endpoints", []string{"POST /search", "POST /extract", "GET /health"})

		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("Server failed to start", "error", err)
			os.Exit(1)
		}
	}()

	// Setup graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	slog.Info("Shutting down server...")

	// Create shutdown context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Shutdown server gracefully
	if err := server.Shutdown(ctx); err != nil {
		slog.Error("Server forced to shutdown", "error", err)
		os.Exit(1)
	}

	slog.Info("Server exited gracefully")
}

// gzipMiddleware remains the same but uses slog for logging
func gzipMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Vary", "Accept-Encoding")
		gw := gzipWriterPool.Get().(*gzip.Writer)
		gw.Reset(w)
		defer func() {
			if err := gw.Close(); err != nil {
				slog.Warn("Error closing gzip writer", "error", err)
			}
			gzipWriterPool.Put(gw)
		}()
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

// timeoutMiddleware remains the same but uses slog for logging
func timeoutMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
		defer cancel()
		r = r.WithContext(ctx)
		done := make(chan struct{})
		go func() {
			next.ServeHTTP(w, r)
			close(done)
		}()
		select {
		case <-done:
			return
		case <-ctx.Done():
			slog.Warn("Request timed out", "method", r.Method, "path", r.URL.Path)
			http.Error(w, "Request timeout", http.StatusGatewayTimeout)
			return
		}
	})
}
