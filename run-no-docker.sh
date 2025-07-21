#!/bin/bash

# Exit immediately if a command exits with a non-zero status.
set -e

# --- Transcript Service (Python) ---
echo "Setting up Python transcript service..."
cd transcript-service

# Create a virtual environment if it doesn't exist
if [ ! -d "venv" ]; then
    python3 -m venv venv
fi

# Activate the virtual environment
source venv/bin/activate

# Check if requirements are already installed
if [ -f "requirements.txt.cached" ] && cmp -s "requirements.txt" "requirements.txt.cached"; then
    echo "Python requirements are already up to date."
else
    echo "Installing Python requirements..."
    pip install -r requirements.txt
    cp requirements.txt requirements.txt.cached
fi

# Start the Python service in the background
echo "Starting transcript service..."
uvicorn main:app --host 0.0.0.0 --port 8000 &
TRANSCRIPT_PID=$!
cd ..

# --- RAG-Forge (Go) ---
echo "Starting RAG-Forge Go application..."
go run main.go &
GO_PID=$!

# --- Shutdown ---
# Wait for either process to exit
wait -n $TRANSCRIPT_PID $GO_PID

# Clean up other process
kill $TRANSCRIPT_PID $GO_PID 2>/dev/null
echo "Services stopped."