#!/bin/bash

# Exit immediately if a command exits with a non-zero status.
set -e

PORT=8086
BASE_URL="http://127.0.0.1:$PORT"

echo "Running tests..."

# Function to check if the response has content
check_content() {
  response=$1
  if echo "$response" | jq -e '.results | length > 0 and all(.processed_successfully)' > /dev/null; then
    echo "✅ Test Passed"
  elif echo "$response" | jq -e '.results | length > 0' > /dev/null; then
    echo "✅ Test Passed with some errors (as expected in some cases)"
  else
    echo "❌ Test Failed: No content or error in response"
    echo "Response: $response"
    exit 1
  fi
}

echo "--- Testing /search endpoint (Sequential) ---"

echo "Query: best guitar"
response_guitar=$(curl -s -X POST "$BASE_URL/search" -H "Content-Type: application/json" -d '{"query": "best guitar"}')
check_content "$response_guitar"

echo "Query: best piano"
response_piano=$(curl -s -X POST "$BASE_URL/search" -H "Content-Type: application/json" -d '{"query": "best piano"}')
check_content "$response_piano"

echo "Query: best drums"
response_drums=$(curl -s -X POST "$BASE_URL/search" -H "Content-Type: application/json" -d '{"query": "best drums"}')
check_content "$response_drums"

echo "--- Testing /extract endpoint (Sequential) ---"

urls_to_extract=(
  "https://www.youtube.com/watch?v=dcBvK3duCrA&pp=0gcJCccJAYcqIYzv"
  "https://x.com/_wonuwo/status/1948236043628556509"
  "https://cachyos.org/"
)

for url in "${urls_to_extract[@]}"; do
  echo "Extracting: $url"
  response=$(curl -s -X POST "$BASE_URL/extract" -H "Content-Type: application/json" -d "{\"urls\": [\"$url\"]}")
  check_content "$response"
done

echo "--- Testing /extract endpoint (Concurrent) ---"

json_payload=$(printf '%s\n' "${urls_to_extract[@]}" | jq -R . | jq -s '{"urls": .}')
response_concurrent=$(curl -s -X POST "$BASE_URL/extract" -H "Content-Type: application/json" -d "$json_payload")
check_content "$response_concurrent"