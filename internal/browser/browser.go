package browser

import (
	"log/slog"
	"sync"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
)

// Pool manages a pool of browser instances.
type Pool struct {
	launcher    *launcher.Launcher
	allBrowsers []*rod.Browser // <-- Track all created browsers
	activePool  chan *rod.Browser
	mu          sync.Mutex
	isClosed    bool // <-- Add a closed flag
}

// NewPool creates and initializes a new browser pool.
func NewPool(size int) (*Pool, error) {
	launcherInstance := NewLauncher()
	launcherURL := launcherInstance.MustLaunch()

	pool := &Pool{
		launcher:    launcherInstance,
		allBrowsers: make([]*rod.Browser, 0, size), // <-- Initialize
		activePool:  make(chan *rod.Browser, size),
	}

	for i := 0; i < size; i++ {
		browser := rod.New().ControlURL(launcherURL).MustConnect()
		pool.allBrowsers = append(pool.allBrowsers, browser) // <-- Track it
		pool.activePool <- browser
	}

	slog.Info("Browser pool initialized", "size", size)
	return pool, nil
}

// Get retrieves a browser from the pool.
func (p *Pool) Get() *rod.Browser {
	return <-p.activePool
}

// Return gives a browser back to the pool.
func (p *Pool) Return(browser *rod.Browser) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.isClosed {
		// If the pool is closed, don't try to return the browser.
		// Just close it directly.
		browser.MustClose()
		return
	}

	p.activePool <- browser
}

// Cleanup closes all browsers in the pool.
func (p *Pool) Cleanup() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.isClosed {
		return // Already cleaned up
	}

	slog.Info("Cleaning up browser pool...")
	p.isClosed = true
	close(p.activePool)

	// Close ALL browsers the pool has ever created, not just the ones in the channel.
	for _, browser := range p.allBrowsers {
		browser.MustClose()
	}
	p.launcher.Cleanup()
	slog.Info("Browser pool cleaned up.")
}

// NewLauncher creates and configures a new Rod launcher with standardized settings.
func NewLauncher() *launcher.Launcher {
	return launcher.New().
		Headless(true).
		Set("--disable-blink-features", "AutomationControlled").
		Set("--no-sandbox").
		Set("--disable-setuid-sandbox").
		Set("--disable-gpu").
		Set("--disable-dev-shm-usage").
		Set("--disable-extensions").
		Set("--disable-plugins-discovery"). // Changed from --disable-plugins
		Set("--disable-images").
		Set("--disable-background-networking").
        // ---- ADD THESE ----
		Set("--disable-background-timer-throttling").
		Set("--disable-backgrounding-occluded-windows").
		Set("--disable-breakpad").
		Set("--disable-client-side-phishing-detection").
		Set("--disable-component-update").
		Set("--disable-default-apps").
		Set("--disable-domain-reliability").
		Set("--disable-features", "AudioServiceOutOfProcess,IsolateOrigins,site-per-process").
		Set("--disable-hang-monitor").
		Set("--disable-ipc-flooding-protection").
		Set("--disable-notifications").
		Set("--disable-popup-blocking").
		Set("--disable-print-preview").
		Set("--disable-prompt-on-repost").
		Set("--disable-renderer-backgrounding").
		Set("--disable-sync").
		Set("--disable-translate").
		Set("--metrics-recording-only").
		Set("--mute-audio").
		Set("--no-first-run").
		Set("--safebrowsing-disable-auto-update").
		Set("--enable-automation").
		Set("--password-store", "basic").
		Set("--use-mock-keychain")
        // Note: --disable-javascript-harmony-shipping is deprecated
}