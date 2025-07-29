package main

import (
	"compress/gzip"
	"context"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"web-search-api-for-llms/internal/api"
	"web-search-api-for-llms/internal/browser"
	"web-search-api-for-llms/internal/cache"
	"web-search-api-for-llms/internal/config"
	"web-search-api-for-llms/internal/extractor"
	"web-search-api-for-llms/internal/worker"

	"github.com/google/uuid"
	_ "github.com/joho/godotenv/autoload" // Automatically load .env file
	goCache "github.com/patrickmn/go-cache"
	_ "go.uber.org/automaxprocs"
	"golang.org/x/sys/unix"
)

var gzipWriterPool = sync.Pool{
	New: func() interface{} {
		return gzip.NewWriter(nil)
	},
}

type contextKey string

const requestIDKey contextKey = "requestID"

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

	// Create a DNS cache
	// Create a DNS cache with manual cleanup
	dnsCache := goCache.New(5*time.Minute, -1)

	// Create a pool of transports. A size of 4 is a good start.
	// This gives you an effective MaxIdleConnsPerHost of 4 * 400 = 1600.
	transportPool := createTransportPool(4, dnsCache)
	var transportCounter uint32

	// Create a single HTTP client that dynamically selects a transport
	httpClient := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &roundRobinTransport{
			transports: transportPool,
			counter:    &transportCounter,
		},
	}

	// Initialize cache based on configuration
	var appCache cache.Cache
	switch appConfig.CacheType {
	case "redis":
		slog.Info("Using Redis cache")
		appCache = cache.NewRedisCache(appConfig.RedisURL, appConfig.RedisPassword, appConfig.RedisDB)
	default:
		slog.Info("Using sharded in-memory cache")
		appCache = cache.NewShardedMemoryCache(10*time.Minute, 15*time.Minute)
	}

	// Create a single dispatcher instance
	dispatcher := extractor.NewDispatcher(appConfig, browserPool, httpClient)

	// A small pool for heavy, CPU-bound browser jobs. Size should match available cores.
	browserWorkerPool := worker.NewWorkerPool(dispatcher, appConfig.BrowserPoolSize, appConfig.BrowserPoolSize*2)
	browserWorkerPool.Start()
	defer browserWorkerPool.Stop()
	slog.Info("Browser worker pool started", "size", appConfig.BrowserPoolSize)

	// A large pool for light, I/O-bound HTTP jobs.
	httpWorkerPool := worker.NewWorkerPool(dispatcher, appConfig.HTTPWorkerPoolSize, appConfig.HTTPWorkerPoolSize*2)
	httpWorkerPool.Start()
	defer httpWorkerPool.Stop()
	slog.Info("HTTP worker pool started", "size", appConfig.HTTPWorkerPoolSize)

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
	requestIDHandler := requestIDMiddleware(handler)

	// Create a custom listener config
	lc := net.ListenConfig{
		Control: func(network, address string, c syscall.RawConn) error {
			var sockOptErr error
			err := c.Control(func(fd uintptr) {
				// Set SO_REUSEPORT on the socket
				sockOptErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEPORT, 1)
			})
			if err != nil {
				return err
			}
			return sockOptErr
		},
	}

	// Use the custom listener config
	listener, err := lc.Listen(context.Background(), "tcp", ":8086")
	if err != nil {
		slog.Error("Failed to create listener", "error", err)
		os.Exit(1)
	}

	server := &http.Server{
		Addr:         ":8086", // Addr is now mainly for reference
		Handler:      requestIDHandler,
		ReadTimeout:  60 * time.Second,
		WriteTimeout: 120 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Start server in a goroutine with the custom listener
	go func() {
		slog.Info("Starting server", "port", 8086)
		slog.Info("Available endpoints", "endpoints", []string{"POST /search", "POST /extract", "GET /health"})

		// Use Serve instead of ListenAndServe
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			slog.Error("Server failed to start", "error", err)
			os.Exit(1)
		}
	}()

	// Manual cache cleanup goroutine
	cleanupTicker := time.NewTicker(10 * time.Minute)
	stopCleanup := make(chan struct{})
	go func() {
		for {
			select {
			case <-cleanupTicker.C:
				slog.Info("Running manual cache cleanup")
				dnsCache.DeleteExpired()
				if shardedCache, ok := appCache.(*cache.ShardedMemoryCache); ok {
					shardedCache.DeleteExpired()
				}
			case <-stopCleanup:
				cleanupTicker.Stop()
				slog.Info("Stopped cache cleanup goroutine")
				return
			}
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

	// Signal the cleanup goroutine to stop
	close(stopCleanup)

	// Shutdown server gracefully
	if err := server.Shutdown(ctx); err != nil {
		slog.Error("Server forced to shutdown", "error", err)
		os.Exit(1)
	}

	slog.Info("Server exited gracefully")
}

// Custom RoundTripper to select a transport from the pool
type roundRobinTransport struct {
	transports []*http.Transport
	counter    *uint32
}

func (r *roundRobinTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	count := atomic.AddUint32(r.counter, 1)
	transportIndex := int(count) % len(r.transports)

	return r.transports[transportIndex].RoundTrip(req)
}

// Create a pool of transports
func createTransportPool(size int, dnsCache *goCache.Cache) []*http.Transport {
	transports := make([]*http.Transport, size)
	for i := 0; i < size; i++ {
		transports[i] = &http.Transport{
			// The DialContext function is now shared across transports,
			// which is fine as the underlying DNS cache is thread-safe.
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				// ... (keep your existing DNS caching logic here)
				host, port, err := net.SplitHostPort(addr)
				if err != nil {
					return nil, err
				}
				if cachedIP, found := dnsCache.Get(host); found {
					return net.Dial(network, net.JoinHostPort(cachedIP.(string), port))
				}
				ips, err := net.LookupHost(host)
				if err != nil {
					return nil, err
				}
				ip := ips[0]
				dnsCache.Set(host, ip, goCache.DefaultExpiration)
				return net.Dial(network, net.JoinHostPort(ip, port))
			},
			MaxIdleConns:          2000,
			MaxIdleConnsPerHost:   400, // This limit is now per-transport
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			ForceAttemptHTTP2:     true,
		}
	}
	return transports
}

func requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := uuid.New().String()
		// Add ID to the response headers for client-side correlation
		w.Header().Set("X-Request-ID", requestID)
		// Create a context with the request ID
		ctx := context.WithValue(r.Context(), requestIDKey, requestID)
		// Update the request with the new context
		r = r.WithContext(ctx)
		next.ServeHTTP(w, r)
	})
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
		slog.Debug("gzipping response")
		gw := gzipWriterPool.Get().(*gzip.Writer)
		gw.Reset(w)
		defer func() {
			if err := gw.Close(); err != nil {
				slog.Warn("Error closing gzip writer", "error", err)
			}
			gzipWriterPool.Put(gw)
			slog.Debug("gzip writer returned to pool")
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

// CloseNotify implements the http.CloseNotifier interface.
func (w *gzipResponseWriter) CloseNotify() <-chan bool {
	return w.ResponseWriter.(http.CloseNotifier).CloseNotify()
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
