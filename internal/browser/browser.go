package browser

import (
	"log"
	"sync"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
)

// Pool manages a pool of browser instances.
type Pool struct {
	launcher *launcher.Launcher
	browsers chan *rod.Browser
	mu       sync.Mutex
}

// NewPool creates and initializes a new browser pool.
func NewPool(size int) (*Pool, error) {
	launcherInstance := NewLauncher()
	launcherURL := launcherInstance.MustLaunch()

	pool := &Pool{
		launcher: launcherInstance,
		browsers: make(chan *rod.Browser, size),
	}

	for i := 0; i < size; i++ {
		browser := rod.New().ControlURL(launcherURL).MustConnect()
		pool.browsers <- browser
	}

	log.Printf("Browser pool initialized with %d instances.", size)
	return pool, nil
}

// Get retrieves a browser from the pool.
func (p *Pool) Get() *rod.Browser {
	return <-p.browsers
}

// Return gives a browser back to the pool.
func (p *Pool) Return(browser *rod.Browser) {
	p.browsers <- browser
}

// Cleanup closes all browsers in the pool.
func (p *Pool) Cleanup() {
	p.mu.Lock()
	defer p.mu.Unlock()

	close(p.browsers)

	for browser := range p.browsers {
		browser.MustClose()
	}
	p.launcher.Cleanup()
	log.Println("Browser pool cleaned up.")
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
		Set("--disable-plugins").
		Set("--disable-images").
		Set("--disable-javascript-harmony-shipping").
		Set("--disable-background-networking")
}