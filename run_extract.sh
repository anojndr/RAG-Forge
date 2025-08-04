#!/bin/bash

# Exit immediately if a command exits with a non-zero status.
set -e

# --- RAG-Forge (Go) ---
# The Go application port is configured in the .env file (defaulting to 8086).
echo "Starting RAG-Forge Go application with only the extract endpoint..."
go run main.go --endpoint=extract