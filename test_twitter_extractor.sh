#!/bin/bash

# Script to test Twitter extractor functionality

echo "========================================="
echo "Twitter Extractor Functionality Tests"
echo "========================================="

# Navigate to the project directory
cd /home/sweetpotet/Desktop/RAG-Forge

# Test 1: Browser Launch and Connection
echo -e "\n1. Testing Browser Launch and Connection..."
go test -v -run TestTwitterExtractorBrowserLaunch ./internal/extractor/

# Test 2: User Agent Setting
echo -e "\n2. Testing User Agent Setting..."
go test -v -run TestTwitterExtractorUserAgent ./internal/extractor/

# Test 3: Page Navigation
echo -e "\n3. Testing Page Navigation..."
go test -v -run TestTwitterExtractorPageNavigation ./internal/extractor/

# Test 4: Element Interaction
echo -e "\n4. Testing Element Interaction..."
go test -v -run TestTwitterExtractorElementInteraction ./internal/extractor/

# Test 5: Cookie Management
echo -e "\n5. Testing Cookie Management..."
go test -v -run TestTwitterExtractorCookieManagement ./internal/extractor/

# Test 6: Tweet ID Extraction
echo -e "\n6. Testing Tweet ID Extraction..."
go test -v -run TestTwitterExtractorTweetIDExtraction ./internal/extractor/

# Test 7: Context Handling
echo -e "\n7. Testing Context Handling..."
go test -v -run TestTwitterExtractorWithContext ./internal/extractor/

echo -e "\n========================================="
echo "All tests completed!"
echo "========================================="

# Run all tests together with coverage
echo -e "\nRunning all tests with coverage report..."
go test -v -cover ./internal/extractor/ -run "TestTwitterExtractor"
