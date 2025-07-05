package utils

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

var (
	pythonCommand     string
	pythonCommandOnce sync.Once
)

// GetPythonCommand returns the appropriate Python command for the current platform
func GetPythonCommand() string {
	pythonCommandOnce.Do(func() {
		pythonCommand = detectPythonCommand()
	})
	return pythonCommand
}

// detectPythonCommand detects the available Python command on the system
func detectPythonCommand() string {
	// List of possible Python commands in order of preference
	candidates := []string{"python3", "python"}

	// On Windows, prefer "python" over "python3"
	if runtime.GOOS == "windows" {
		candidates = []string{"python", "python3"}
	}

	for _, cmd := range candidates {
		if isPythonCommandValid(cmd) {
			return cmd
		}
	}

	// Default fallback
	return "python3"
}

// isPythonCommandValid checks if a Python command is valid and has the right version
func isPythonCommandValid(cmd string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Test if command exists and is Python 3
	execCmd := exec.CommandContext(ctx, cmd, "--version")
	output, err := execCmd.Output()
	if err != nil {
		return false
	}

	version := strings.TrimSpace(string(output))
	return strings.HasPrefix(version, "Python 3.")
}

// ValidateSystemDependencies checks if required system dependencies are available
func ValidateSystemDependencies() error {
	// Check Python
	pythonCmd := GetPythonCommand()
	if !isPythonCommandValid(pythonCmd) {
	return fmt.Errorf("python 3 not found (tried: %s)", pythonCmd)
	}

	// Check pip
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pipCmd := exec.CommandContext(ctx, pythonCmd, "-m", "pip", "--version")
	if err := pipCmd.Run(); err != nil {
		return fmt.Errorf("pip not available with Python command: %s", pythonCmd)
	}

	// Check pdftotext
	pdfCmd := exec.CommandContext(ctx, "pdftotext", "-v")
	if err := pdfCmd.Run(); err != nil {
		return fmt.Errorf("pdftotext not found (install poppler-utils)")
	}

	return nil
}

// EnsureVenvExists creates a virtual environment if it doesn't exist
func EnsureVenvExists() error {
	venvDir := "./venv"
	venvPython := "./venv/bin/python"
	if runtime.GOOS == "windows" {
		venvPython = "./venv/Scripts/python.exe"
	}

	// Check if venv already exists
	if _, err := exec.LookPath(venvPython); err == nil {
		return nil // venv already exists
	}

	// Create venv
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pythonCmd := GetPythonCommand()
	cmd := exec.CommandContext(ctx, pythonCmd, "-m", "venv", venvDir)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to create virtual environment: %w", err)
	}

	return nil
}

// InstallPythonPackage installs a Python package using pip in venv only
func InstallPythonPackage(packageName string) error {
	// Ensure venv exists first
	if err := EnsureVenvExists(); err != nil {
		return fmt.Errorf("failed to ensure venv exists: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Use venv Python only - never install outside venv
	venvPython := "./venv/bin/python"
	if runtime.GOOS == "windows" {
		venvPython = "./venv/Scripts/python.exe"
	}

	// Verify venv exists
	if _, err := exec.LookPath(venvPython); err != nil {
		return fmt.Errorf("virtual environment not found at %s", venvPython)
	}

	args := []string{"-m", "pip", "install", "--quiet", packageName}
	cmd := exec.CommandContext(ctx, venvPython, args...)
	return cmd.Run()
}
