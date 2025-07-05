This version provides more detailed explanations of the intelligent extraction features, clarifies the automatic dependency installation, and expands the troubleshooting section.

```markdown
# RAG-Forge: Complete API Documentation

Welcome to the complete documentation for RAG-Forge, a web content extraction API for RAG pipelines. This guide covers everything from installation and configuration to detailed API usage and development practices.

## Table of Contents

1.  [Features](#features)
2.  [Prerequisites & Installation](#prerequisites--installation)
3.  [Configuration](#configuration)
4.  [Running the API](#running-the-api)
5.  [API Reference](#api-reference)
    - [POST /search](#post-search)
    - [POST /extract](#post-extract)
    - [GET /health](#get-health)
    - [Response Objects](#response-objects)
6.  [Caching](#caching)
7.  [Code Examples](#code-examples)
    - [Python Example](#python-example)
    - [JavaScript Example](#javascript-example)
8.  [Troubleshooting](#troubleshooting)
9.  [Development & Testing](#development--testing)

---

## Features

-   **Dual Extraction Modes**: Search the web with a query (`/search`) or provide URLs directly (`/extract`).
-   **Multi-Source Support**: Automatically extracts content from YouTube, Reddit, Twitter/X, PDFs, and standard webpages.
-   **Flexible Search Backend**: Use a self-hosted **SearxNG** instance or the commercial **Serper.dev** API. Supports a primary and fallback configuration.
-   **Intelligent Extraction**:
    -   **Twitter/X**: Uses browser automation to log in and scrape full post content and comments, bypassing public API limitations. Session cookies are saved to accelerate subsequent requests.
    -   **YouTube**: Fetches metadata/comments via the official API and uses a robust, configurable transcript extractor (`youtube-transcript-api` or Tactiq fallback) with automatic Python dependency management in a local `venv`.
    -   **Reddit**: Intelligently parses different Reddit URL types (posts, subreddits, user profiles) and extracts actual comment content, filtering out "load more" placeholders.
-   **Performance Optimized**: Utilizes concurrent processing for multiple URLs and a two-level caching system for both search queries and URL content.
-   **Structured Output**: Returns clean, structured JSON data, perfect for feeding into LLMs or RAG systems.
-   **Robust Error Handling**: Provides clear error messages within the JSON response without halting the entire process.
-   **Health Check**: Includes a `/health` endpoint for easy integration with monitoring and orchestration tools.

## Prerequisites & Installation

Before you begin, ensure you have the following dependencies installed and available in your system's PATH.

### 1. System Dependencies

-   **Go**: Version 1.23.1 or higher.
-   **Python**: Version 3.8 or higher, along with `pip`. Required for the YouTube transcript extractor's automatic setup.
-   **poppler-utils**: Provides `pdftotext` for PDF extraction.
    -   **On Ubuntu/Debian**: `sudo apt-get update && sudo apt-get install -y poppler-utils`
    -   **On macOS (Homebrew)**: `brew install poppler`
-   **Chromium-based Browser**: Required for Twitter/X extraction. The extractor uses browser automation to scrape content.
    -   Install Google Chrome, Chromium, or another compatible browser.

### 2. Python Packages
The application now manages its own Python dependencies in a local virtual environment (`venv`) folder. **You no longer need to install `youtube-transcript-api` manually.** The API will create the `venv` and install the package on its first run if needed.

### 3. Application Setup

1.  **Clone the Repository:**
    ```bash
    git clone https://github.com/your-username/RAG-Forge.git
    cd RAG-Forge
    ```

2.  **Install Go Dependencies:**
     The project uses Go Modules. Dependencies will be fetched automatically when you build or run the project.

## Configuration

Configuration is managed via a `.env` file in the project root.

1.  **Create a `.env` file**:
    ```bash
    cp ENV_EXAMPLE.TXT .env
    ```

2.  **Edit the `.env` file** with your specific settings.

### Environment Variables

| Variable                  | Description                                                                                               | Default                            | Required?                          |
| ------------------------- | --------------------------------------------------------------------------------------------------------- | ---------------------------------- | ---------------------------------- |
| `PORT`                    | The port on which the API server will listen.                                                             | `8080`                             | No                                 |
| `MAIN_SEARCH_ENGINE`      | The primary search engine to use. Can be `searxng` or `serper`.                                           | `searxng`                          | No                                 |
| `FALLBACK_SEARCH_ENGINE`  | The fallback engine if the primary fails. Can be `searxng`, `serper`, or empty.                           | `serper`                           | No                                 |
| `SEARXNG_URL`             | The URL of your running SearxNG instance.                                                                 | `http://localhost:18088`           | If using `searxng`                 |
| `SERPER_API_KEY`          | Your API key from [serper.dev](https://serper.dev/).                                                      | (none)                             | If using `serper`                  |
| `SERPER_API_URL`          | The API endpoint for Serper.                                                                              | `https://google.serper.dev/search` | If using `serper`                  |
| `YOUTUBE_API_KEY`         | Your YouTube Data API v3 key for fetching video details and comments.                                     | (none)                             | For YouTube features               |
| `YOUTUBE_TRANSCRIPT_ORDER`| Comma-separated order of transcript extraction methods. Valid entries: `ytapi`, `tactiq`.                   | `ytapi,tactiq`                     | No                                 |
| `REDDIT_CLIENT_ID`        | Your Reddit app client ID.                                                                                | (none)                             | For Reddit features                |
| `REDDIT_CLIENT_SECRET`    | Your Reddit app client secret.                                                                            | (none)                             | For Reddit features                |
| `REDDIT_USER_AGENT`       | A descriptive user-agent for Reddit API requests.                                                         | (randomized)                       | For Reddit features                |
| `TWITTER_USERNAME`        | Your Twitter/X username or email for logging in.                                                          | (none)                             | For Twitter/X features             |
| `TWITTER_PASSWORD`        | Your Twitter/X password.                                                                                  | (none)                             | For Twitter/X features             |
| `WEBSHARE_PROXY_USERNAME` | Webshare proxy username, used by the YouTube `ytapi` extractor.                                           | (none)                             | Optional                           |
| `WEBSHARE_PROXY_PASSWORD` | Webshare proxy password, used by the YouTube `ytapi` extractor.                                           | (none)                             | Optional                           |

## Running the API

With your dependencies installed and `.env` file configured, start the server with:

```bash
go run main.go
```

The server will log its startup status, port, and available endpoints to the console.

## API Reference

The API serves JSON and follows standard HTTP conventions. A successful request to `/search` or `/extract` will return a `200 OK` status, even if some individual URLs failed to process. Failures for specific URLs are reported within the `results` array of the JSON response.

### POST /search

Performs a web search using the configured search engine, then extracts content from the top results.

-   **Method**: `POST`
-   **Path**: `/search`
-   **Request Body**: `application/json`

**Request Payload:**
| Field              | Type    | Description                                                                   | Required |
| ------------------ | ------- | ----------------------------------------------------------------------------- | -------- |
| `query`            | string  | The search query.                                                             | Yes      |
| `max_results`      | integer | The maximum number of search results to process. Defaults to 10.                | No       |
| `max_char_per_url` | integer | Optional. Truncates the content of each result to this character limit.         | No       |

**Example Request:**
```bash
curl -X POST http://localhost:8080/search \
-H "Content-Type: application/json" \
-d '{
  "query": "What is Retrieval-Augmented Generation?",
  "max_results": 5,
  "max_char_per_url": 8000
}'
```

**Response**: Returns a `FinalResponsePayload` object. See [Response Objects](#response-objects).

### POST /extract

Extracts content directly from a list of provided URLs.

-   **Method**: `POST`
-   **Path**: `/extract`
-   **Request Body**: `application/json`

**Request Payload:**
| Field              | Type           | Description                                                              | Required |
| ------------------ | -------------- | ------------------------------------------------------------------------ | -------- |
| `urls`             | array of strings | An array of URLs to extract content from. Maximum of 20 URLs per request. | Yes      |
| `max_char_per_url` | integer        | Optional. Truncates the content of each result to this character limit.    | No       |

**Example Request:**
```bash
curl -X POST http://localhost:8080/extract \
-H "Content-Type: application/json" \
-d '{
  "urls": [
    "https://www.youtube.com/watch?v=...",
    "https://www.reddit.com/r/MachineLearning/comments/...",
    "https://x.com/some_user/status/1234567890"
  ]
}'
```

**Response**: Returns an `ExtractResponsePayload` object. See [Response Objects](#response-objects).

### GET /health

A simple endpoint to check if the API server is running.

-   **Method**: `GET`
-   **Path**: `/health`

**Example Request:**
```bash
curl http://localhost:8080/health
```

**Example Response (`200 OK`):**
```json
{
  "status": "healthy",
  "timestamp": "2023-10-27T10:00:00Z"
}
```

### Response Objects

#### `ExtractedResult`
This object represents the outcome of processing a single URL. It's a common component in both response payloads.

| Field                   | Type        | Description                                                                                             |
| ----------------------- | ----------- | ------------------------------------------------------------------------------------------------------- |
| `url`                   | string      | The URL that was processed.                                                                             |
| `source_type`           | string      | The detected type of content: `youtube`, `reddit`, `pdf`, `twitter`, or `webpage`.                      |
| `processed_successfully`| boolean     | `true` if content was extracted successfully, `false` otherwise.                                        |
| `data`                  | object      | The extracted content. The structure of this object depends on the `source_type`. See below.            |
| `error`                 | string      | An error message if `processed_successfully` is `false`. `null` otherwise.                              |

**`data` Object Structures by `source_type`:**
-   **`webpage`**: `{ "title": "...", "text_content": "..." }`
-   **`pdf`**: `{ "text_content": "..." }`
-   **`youtube`**: `{ "title": "...", "channel_name": "...", "transcript": "...", "comments": [...] }`
-   **`reddit`**: `{ "post_title": "...", "post_body": "...", "score": ..., "author": "...", "comments": [...] }`
-   **`twitter`**: `{ "tweet_content": "...", "tweet_author": "...", "comments": [...], "total_comments": ... }`

**Reddit Comments Structure:**
Each comment in the `comments` array contains:
```json
{
  "author": "username",
  "score": 123,
  "text": "actual comment content",
  "replies": [...] // nested replies (if any)
}
```
The system extracts up to 50 top-level comments with their actual content, filtering out Reddit's pagination objects that would otherwise show as "... and X more comments".

**Twitter Comment Structure:**
Each comment in the `comments` array for a Twitter result contains:
```json
{
  "author": "Display Name",
  "username": "username",
  "content": "The text of the reply.",
  "timestamp": "e.g., '1h' or 'Dec 25, 2023'",
  "likes": "123",
  "replies": "45",
  "retweets": "67"
}
```

#### `FinalResponsePayload` (for `/search`)
```json
{
  "query_details": {
    "query": "your search query",
    "max_results_requested": 5,
    "actual_results_found": 5
  },
  "results": [
    // Array of ExtractedResult objects
  ],
  "error": "Error message if the search itself failed, null otherwise"
}
```

#### `ExtractResponsePayload` (for `/extract`)
```json
{
  "request_details": {
    "urls_requested": 3,
    "urls_processed": 3
  },
  "results": [
    // Array of ExtractedResult objects
  ],
  "error": "Error message if the request was malformed, null otherwise"
}
```

## Live Demo & Example Integration

You can see this API in action by checking out the **Discord AI Chatbot**, which uses RAG-Forge as its primary tool for web content extraction. This provides a real-world example of how to integrate RAG-Forge into an application to power its knowledge-gathering capabilities.

*   **Discord AI Chatbot**: [`https://github.com/anojndr/Discord_AI_chatbot`](https://github.com/anojndr/Discord_AI_chatbot)

## Code Examples

### Python Example
```python
import requests
import json

API_BASE_URL = "http://localhost:8080"

def search_content(query: str, max_results: int = 5):
    """Search and extract content using the /search endpoint."""
    try:
        response = requests.post(
            f"{API_BASE_URL}/search",
            json={"query": query, "max_results": max_results},
            timeout=120  # Increased timeout for complex searches
        )
        response.raise_for_status()
        return response.json()
    except requests.RequestException as e:
        print(f"An error occurred: {e}")
        return None

def extract_from_urls(urls: list):
    """Extract content directly from a list of URLs."""
    try:
        response = requests.post(
            f"{API_BASE_URL}/extract",
            json={"urls": urls},
            timeout=120  # Increased timeout for complex extractions
        )
        response.raise_for_status()
        return response.json()
    except requests.RequestException as e:
        print(f"An error occurred: {e}")
        return None

if __name__ == "__main__":
    # Example 1: Search for a topic
    search_results = search_content("latest advancements in AI for drug discovery")
    if search_results and search_results.get("results"):
        print(f"Found {len(search_results['results'])} results.")
        for res in search_results["results"]:
            if res["processed_successfully"]:
                print(f"✅ Success: {res['url']} ({res['source_type']})")
            else:
                print(f"❌ Failed: {res['url']} - Error: {res['error']}")
    
    # Example 2: Extract from specific URLs
    url_list = [
        "https://x.com/ylecun/status/1733222384223129841",
        "https://www.reddit.com/r/MachineLearning/comments/1chyvj1/d_what_are_the_most_impressive_llm_demos_youve/"
    ]
    extracted_data = extract_from_urls(url_list)
    if extracted_data:
        print("\nExtraction from specific URLs:")
        for res in extracted_data["results"]:
            print(f"Processed: {res['url']}")
```

### JavaScript Example
```javascript
const API_BASE_URL = 'http://localhost:8080';

async function searchContent(query, maxResults = 5) {
  try {
    const response = await fetch(`${API_BASE_URL}/search`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({ query, max_results: maxResults }),
    });
    if (!response.ok) {
      throw new Error(`HTTP error! status: ${response.status}`);
    }
    return await response.json();
  } catch (error) {
    console.error('Error fetching search content:', error);
    return null;
  }
}

// Example usage:
searchContent('Common Lisp vs. Scheme')
  .then(data => {
    if (data && data.results) {
      console.log('Search Results:', data.results);
      data.results.forEach(result => {
        if (result.processed_successfully) {
          console.log(`Successfully processed ${result.url}`);
        }
      });
    }
  });
```

## Troubleshooting

-   **"No results found" for `/search`**:
    -   Verify your `MAIN_SEARCH_ENGINE` is configured correctly in `.env`.
    -   If using SearxNG, ensure the instance at `SEARXNG_URL` is running and accessible from the API server.
    -   If using Serper, double-check that `SERPER_API_KEY` is correct and has not expired.
-   **Twitter/X extraction fails**:
    -   Ensure a Chromium-based browser is installed and accessible in the system's environment.
    -   Make sure your `TWITTER_USERNAME` and `TWITTER_PASSWORD` are correct.
    -   On the first run, the API will create a browser session and save cookies to `twitter_cookies.json`. Subsequent runs will be faster. If login fails repeatedly, you may need to **delete `twitter_cookies.json`** to force a fresh login.
    -   Headless browsing can sometimes be blocked or face CAPTCHAs.
-   **YouTube extraction fails or has no transcript**:
    -   Ensure Python 3.8+ and `pip` are installed. The API will attempt to create a `venv` and install `youtube-transcript-api` automatically. Check the console logs for any Python or `pip` errors.
    -   Not all videos have transcripts. The API will still attempt to get metadata and comments.
-   **PDF extraction fails**:
    -   Ensure `pdftotext` (from `poppler-utils`) is installed and in your system's PATH.
-   **Server fails to start with "address already in use"**:
    -   Another process is using the `PORT` defined in your `.env` file. Stop the other process or change the port.

## Development & Testing

Use standard Go commands for development (e.g., `go build`, `go test`, `go run main.go`). The Makefile and related commands have been removed to simplify the toolchain.