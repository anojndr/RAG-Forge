#!/bin/bash
set -e

PORT=8086
BASE_URL="http://127.0.0.1:$PORT"

echo ""
echo "================================="
echo " Running RAG-Forge Smoke Tests"
echo "================================="
echo ""

echo "--- [1/3] Testing 'search' endpoint ---"
echo "Query: best guitar"
curl -f -s -X POST "$BASE_URL/search" -H "Content-Type: application/json" -d '{"query": "best guitar"}'
echo "" && echo "✅ Command Executed."
echo ""

echo "--- [2/3] Testing 'extract' endpoint (Single URL) ---"
echo "Extracting: https://cachyos.org/"
curl -f -s -X POST "$BASE_URL/extract" -H "Content-Type: application/json" -d '{"urls": ["https://cachyos.org/"]}'
echo "" && echo "✅ Command Executed."
echo ""

echo "--- [3/3] Testing 'extract' endpoint (Multiple URLs) ---"
echo "Extracting multiple URLs..."
curl -f -s -X POST "$BASE_URL/extract" -H "Content-Type: application/json" -d '{"urls": ["https://www.youtube.com/watch?v=dcBvK3duCrA", "https://cachyos.org/"]}'
echo "" && echo "✅ Command Executed."
echo ""

echo "================================="
echo " All test commands executed successfully."
echo " Please verify the output manually."
echo "================================="
echo ""