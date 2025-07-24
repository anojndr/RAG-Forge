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
7.  [Live Demo & Example Integration](#live-demo--example-integration)
8.  [Code Examples](#code-examples)
    - [Python Example](#python-example)
9.  [Troubleshooting](#troubleshooting)
10. [Development & Testing](#development--testing)

---

## Features

-   **Dual Extraction Modes**:
    -   `/search`: **Optimized for speed.** Uses a fast, non-JS-rendering extractor for quickly processing large volumes of standard web articles from search results.
    -   `/extract`: **Optimized for compatibility.** Uses a full JS-enabled headless browser to handle complex, dynamic sites, single-page apps, and sources like Twitter/X.
-   **Multi-Source Support**: Automatically extracts content from YouTube (videos), Reddit (posts, subreddits, user profiles), Twitter/X (tweets and user profiles), PDFs, and standard webpages.
-   **Flexible Search Backend**: Use a self-hosted **SearxNG** instance or the commercial **Serper.dev** API. Supports a primary and fallback configuration.
-   **Intelligent Extraction**:
    -   **Twitter/X**: Uses browser automation to log in and scrape full post content and comments. The `/extract` endpoint can also fetch the latest ~5 tweets from a user's profile page. Session cookies are saved to accelerate subsequent requests.
    -   **YouTube**: Fetches metadata/comments via the official API and gets full transcripts from a dedicated Python microservice for high performance and reliability.
    -   **Reddit**: Intelligently parses different Reddit URL types (posts, subreddits, user profiles) and extracts actual content, filtering out "load more" placeholders.
-   **Decoupled & Optimized Architecture**:
    -   **Transcript Microservice**: YouTube transcript extraction is handled by a separate, containerized Python FastAPI service, improving performance, scalability, and stability.
    -   **Dual Worker Pools**: Employs two separate worker pools—a large one for fast, I/O-bound jobs (standard webpages, APIs) and a smaller one for slow, CPU-bound jobs (JS rendering, Twitter/X)—to ensure high throughput.
    -   **Advanced Caching**: Implements a two-level caching system (sharded in-memory or Redis) with batched reads/writes for both search queries and URL content.
-   **Structured Output**: Returns clean, structured JSON data, perfect for feeding into LLMs or RAG systems.
-   **Health Check**: Includes a `/health` endpoint for easy integration with monitoring and orchestration tools.

## Prerequisites & Installation

Before you begin, ensure you have the following dependencies installed.

### System Dependencies

-   **Go**: Version 1.23.1 or higher.
-   **`pdftotext`**: Required for PDF text extraction. On Debian/Ubuntu, install with `sudo apt-get install poppler-utils`.
-   **Chromium-based Browser**: Required for the `/extract` endpoint and Twitter/X features. Install Google Chrome, Chromium, or another compatible browser.

### Application Setup

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
| `PORT`                    | The port for the API server. *Note: The port is currently hardcoded to `8086` in `main.go` and `docker-compose.yml`.* | `8080`                             | No                                 |
| `MAIN_SEARCH_ENGINE`      | The primary search engine. Can be `searxng` or `serper`.                                                    | `searxng`                          | No                                 |
| `FALLBACK_SEARCH_ENGINE`  | The fallback engine if the primary fails. Can be `searxng`, `serper`, or empty.                           | `serper`                           | No                                 |
| `SEARXNG_URL`             | The URL of your running SearxNG instance.                                                                 | `http://127.0.0.1:18088`           | If using `searxng`                 |
| `SERPER_API_KEY`          | Your API key from [serper.dev](https://serper.dev/).                                                      | (none)                             | If using `serper`                  |
| `TRANSCRIPT_SERVICE_URL`  | The URL of the Python transcript microservice.                                                            | `http://127.0.0.1:8000`            | No                                 |
| `YOUTUBE_API_KEY`         | Your YouTube Data API v3 key for fetching video details and comments.                                     | (none)                             | For YouTube features               |
| `YOUTUBE_TRANSCRIPT_ORDER`| Comma-separated order of transcript methods. Valid entries: `ytapi` (microservice), `tactiq`.           | `ytapi,tactiq`                     | No                                 |
| `REDDIT_CLIENT_ID`        | Your Reddit app client ID.                                                                                | (none)                             | For Reddit features                |
| `REDDIT_CLIENT_SECRET`    | Your Reddit app client secret.                                                                            | (none)                             | For Reddit features                |
| `TWITTER_USERNAME`        | Your Twitter/X username or email for logging in.                                                          | (none)                             | For Twitter/X features             |
| `TWITTER_PASSWORD`        | Your Twitter/X password.                                                                                  | (none)                             | For Twitter/X features             |
| `CACHE_TYPE`              | Cache type to use. Valid values: `memory`, `redis`.                                                       | `memory`                           | No                                 |
| `REDIS_URL`               | Redis connection URL.                                                                                     | `127.0.0.1:6379`                   | If using `redis`                   |
| `SEARCH_CACHE_TTL`        | Cache duration for search results (e.g., `10m`, `1h`).                                                     | `10m`                              | No                                 |
| `CONTENT_CACHE_TTL`       | Cache duration for extracted content.                                                                     | `1h`                               | No                                 |
| `HTTP_WORKER_POOL_SIZE`   | Number of concurrent workers for I/O-bound tasks (webpages, APIs).                                        | `1000`                             | No                                 |
| `BROWSER_POOL_SIZE`       | Number of concurrent workers for CPU-bound tasks (JS rendering). Should be close to CPU cores.            | `6`                                | No                                 |

## Running the API

The API server runs on port `8086`. There are two recommended ways to run the application and its microservice.

A convenience script, `run.sh`, is provided to set up and run both services locally. This script will:
1. Create a Python virtual environment for the transcript service.
2. Install its dependencies from `requirements.txt`.
3. Start the Python service in the background.
4. Start the Go API server.

To use it, simply run:
```bash
./run.sh
```
The script handles graceful shutdown of both processes when you stop it (e.g., with `Ctrl+C`).

## API Reference

The API serves JSON and follows standard HTTP conventions. A successful request to `/search` or `/extract` will return a `200 OK` status, even if some individual URLs failed to process. Failures are reported within the `results` array of the JSON response.

### POST /search

Performs a web search, then extracts content from the top results. This endpoint is **optimized for speed** and uses a fast, **non-JS-rendering** extractor. It is best for processing standard articles and blogs from search results.

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
curl -X POST http://127.0.0.1:8086/search \
-H "Content-Type: application/json" \
-d '{
  "query": "What is Retrieval-Augmented Generation?",
  "max_results": 5,
  "max_char_per_url": 8000
}'
```

**Response**: Returns a `FinalResponsePayload` object. See [Response Objects](#response-objects).

### POST /extract

Extracts content directly from a list of provided URLs. This endpoint is **optimized for compatibility** and **always uses a JS-enabled headless browser** to ensure it can handle modern, dynamic websites (like Twitter/X or single-page apps).

-   **Method**: `POST`
-   **Path**: `/extract`
-   **Request Body**: `application/json`

**Request Payload:**
| Field                   | Type           | Description                                                                   | Required |
| ----------------------- | -------------- | ----------------------------------------------------------------------------- | -------- |
| `urls`                  | array of strings | An array of URLs to extract content from. Maximum of 20 URLs per request.     | Yes      |
| `max_char_per_url`      | integer        | Optional. Truncates the content of each result to this character limit.         | No       |

**Example Request:**
```bash
curl -X POST http://127.0.0.1:8086/extract \
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
curl http://127.0.0.1:8086/health
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
This object represents the outcome of processing a single URL.

| Field                   | Type        | Description                                                                                             |
| ----------------------- | ----------- | ------------------------------------------------------------------------------------------------------- |
| `url`                   | string      | The URL that was processed.                                                                             |
| `source_type`           | string      | Detected type: `youtube`, `reddit`, `pdf`, `twitter`, `twitter_profile`, `webpage`, or `webpage_js`. |
| `processed_successfully`| boolean     | `true` if content was extracted successfully, `false` otherwise.                                        |
| `data`                  | object      | The extracted content. The structure depends on the `source_type`. See below.                         |
| `error`                 | string      | An error message if `processed_successfully` is `false`. `null` otherwise.                              |

**`data` Object Structures by `source_type`:**
-   **`webpage` / `webpage_js`**: `{ "title": "...", "text_content": "..." }`
-   **`pdf`**: `{ "text_content": "..." }`
-   **`youtube`**: `{ "title": "...", "channel_name": "...", "transcript": "...", "comments": [...] }`
-   **`reddit`**: `{ "post_title": "...", "post_body": "...", "author": "...", "comments": [...], "posts": [...] }` (Either `comments` or `posts` will be populated for posts vs. subreddit/user profiles).
-   **`twitter`**: `{ "tweet_content": "...", "tweet_author": "...", "comments": [...], "total_comments": ... }`
-   **`twitter_profile`**: `{ "profile_url": "...", "latest_tweets": [ { "url": "...", "data": <TwitterData> }, ... ] }`

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

## Caching
RAG-Forge implements a two-level caching strategy to improve performance and reduce redundant work:
1.  **Search Cache:** Caches the list of URLs returned by a `/search` query. TTL is configured via `SEARCH_CACHE_TTL`.
2.  **Content Cache:** Caches the `ExtractedResult` object for a specific URL. TTL is configured via `CONTENT_CACHE_TTL`.

The cache can be run using a high-performance, sharded in-memory cache (`CACHE_TYPE=memory`) or with Redis (`CACHE_TYPE=redis`) for persistence and batched operations (`MGET`/`MSET`).

## Live Demo & Example Integration

You can see this API in action by checking out the **Discord AI Chatbot**, which uses RAG-Forge as its primary tool for web content extraction.

*   **Discord AI Chatbot**: [`https://github.com/anojndr/Discord_AI_chatbot`](https://github.com/anojndr/Discord_AI_chatbot)

## Code Examples

### Python Example
```python
import requests
import json

API_BASE_URL = "http://127.0.0.1:8086"

def search_content(query: str, max_results: int = 5):
    """Search and extract content using the /search endpoint."""
    try:
        response = requests.post(
            f"{API_BASE_URL}/search",
            json={"query": query, "max_results": max_results},
            timeout=120
        )
        response.raise_for_status()
        return response.json()
    except requests.RequestException as e:
        print(f"An error occurred: {e}")
        return None

if __name__ == "__main__":
    search_results = search_content("latest advancements in AI for drug discovery")
    if search_results and search_results.get("results"):
        print(f"Found {len(search_results['results'])} results.")
        for res in search_results["results"]:
            if res["processed_successfully"]:
                print(f"✅ Success: {res['url']} ({res['source_type']})")
                # Example of accessing data
                if res['data'] and 'title' in res['data']:
                    print(f"   Title: {res['data']['title']}")
            else:
                print(f"❌ Failed: {res['url']} - Error: {res['error']}")
```

## Troubleshooting

-   **"No results found" for `/search`**:
    -   Verify your `MAIN_SEARCH_ENGINE` is configured correctly in `.env`.
    -   If using SearxNG, ensure the instance at `SEARXNG_URL` is running and accessible.
    -   If using Serper, double-check that `SERPER_API_KEY` is correct.
-   **Twitter/X extraction fails**:
    -   Ensure a Chromium-based browser is installed and accessible in your system's PATH.
    -   Make sure your `TWITTER_USERNAME` and `TWITTER_PASSWORD` are correct.
    -   On the first run, the API saves login cookies to `twitter_cookies.json`. If login fails repeatedly, **delete `twitter_cookies.json`** to force a fresh login attempt.
-   **YouTube transcript extraction fails**:
	-   Ensure the service is running and accessible from the main Go application at the `TRANSCRIPT_SERVICE_URL`.
-   **Server fails to start with "address already in use"**:
	-   Another process is using port `8086`. Stop the other process or change the hardcoded port in `main.go`.

## Development & Testing

Use standard Go commands for development (`go build`, `go test`, `go run main.go`). The build and run process is now primarily managed through the provided `run.sh` script.