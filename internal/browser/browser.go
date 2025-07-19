package browser

import (
	"github.com/go-rod/rod/lib/launcher"
)

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