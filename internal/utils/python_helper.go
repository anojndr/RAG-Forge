package utils

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os/exec"
	"runtime"
	"sync"
)

// PythonHelper manages a long-lived Python helper process.
type PythonHelper struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	mu     sync.Mutex
}

// NewPythonHelper creates and starts a new Python helper process.
func NewPythonHelper(scriptPath string) (*PythonHelper, error) {
	pythonCmd := "./venv/bin/python"
	if runtime.GOOS == "windows" {
		pythonCmd = ".\\venv\\Scripts\\python.exe"
	}

	cmd := exec.Command(pythonCmd, scriptPath)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to get stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to get stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start python helper: %w", err)
	}

	return &PythonHelper{
		cmd:    cmd,
		stdin:  stdin,
		stdout: stdout,
	}, nil
}

// SendRequest sends a request to the Python helper process and returns the response.
func (h *PythonHelper) SendRequest(request interface{}) (map[string]interface{}, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	reqBytes, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	if _, err := h.stdin.Write(append(reqBytes, '\n')); err != nil {
		return nil, fmt.Errorf("failed to write to python helper stdin: %w", err)
	}

	reader := bufio.NewReader(h.stdout)
	line, err := reader.ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("failed to read from python helper stdout: %w", err)
	}

	var response map[string]interface{}
	if err := json.Unmarshal(line, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return response, nil
}

// Close terminates the Python helper process.
func (h *PythonHelper) Close() {
	if h.cmd != nil && h.cmd.Process != nil {
		if err := h.cmd.Process.Kill(); err != nil {
			log.Printf("Failed to kill python helper process: %v", err)
		}
	}
}