#!/bin/bash

# Exit immediately if a command exits with a non-zero status.
set -e

# --- PIDs of background processes ---
TRANSCRIPT_PID=
GO_PID=

# --- Cleanup function ---
# This function is called when the script exits (on any signal or normal exit)
# to ensure that all background services are properly shut down.
cleanup() {
    echo "" # Add a newline for cleaner exit messages
    echo "Shutting down services..."
    # The `|| true` prevents the script from exiting if the process is already gone
    if [ -n "$GO_PID" ]; then
        kill $GO_PID 2>/dev/null || true
    fi
    if [ -n "$TRANSCRIPT_PID" ]; then
        kill $TRANSCRIPT_PID 2>/dev/null || true
    fi
    echo "Services stopped."
}

# Trap the EXIT signal to call the cleanup function. This ensures that no matter
# how the script terminates (e.g., Ctrl+C, error, or normal completion), the
# background processes will be stopped.
trap cleanup EXIT

# --- Transcript Service (Python) ---
echo "Setting up Python transcript service..."
cd transcript-service

# Create a virtual environment if it doesn't exist to isolate dependencies.
if [ ! -d "venv" ]; then
    echo "Creating Python virtual environment..."
    python3 -m venv venv
fi

# Activate the virtual environment
source venv/bin/activate

# Check if requirements are already installed to avoid redundant installations.
# This speeds up subsequent runs of the script.
if [ -f "requirements.txt.cached" ] && cmp -s "requirements.txt" "requirements.txt.cached"; then
    echo "Python requirements are up-to-date."
else
    echo "Installing/updating Python requirements..."
    pip install -r requirements.txt
    # Cache the requirements file to check for changes next time.
    cp requirements.txt requirements.txt.cached
fi

# Start the Python service in the background
echo "Starting transcript service on http://127.0.0.1:8000..."
uvicorn main:app --host 127.0.0.1 --port 8000 &
TRANSCRIPT_PID=$!
cd ..

# --- RAG-Forge (Go) ---
# The Go application port is configured in the .env file (defaulting to 8086).
echo "Starting RAG-Forge Go application..."
go run main.go &
GO_PID=$!

# --- Wait for Application ---
# The script will now wait for the main Go application process to exit.
# If the Go application crashes or is stopped, the script will proceed to exit.
# When the script exits, the 'trap' will trigger the 'cleanup' function to stop
# the background Python service.
echo "RAG-Forge is running. Press Ctrl+C to stop all services."
wait $GO_PID