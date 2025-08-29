# RAG-Forge: Web Content Extraction API

## Overview

RAG-Forge is a powerful, self-hosted Go API designed to fetch and extract clean, structured content from various web sources. It's built to be a reliable data-gathering tool for RAG (Retrieval-Augmented Generation) pipelines and other applications that need to consume web content.

The API is architected for performance and compatibility, featuring a decoupled Python microservice for YouTube transcripts and two distinct extraction modes:
- **Fast Mode (`/search`)**: Give it a search query, and it uses a configured search engine (SearxNG or Serper) to find relevant URLs, then extracts their content using a lightweight, non-JS-rendering scraper for maximum speed.
- **Compatibility Mode (`/extract`)**: Provide a list of specific URLs, and it extracts the content directly using a full, JS-enabled headless browser to handle complex and dynamic websites.

## Key Features

*   **Dual Extraction Modes**:
    *   `POST /search`: Searches the web and extracts content from the top results using a fast, non-JS-rendering scraper. Ideal for processing articles and blogs at scale.
    *   `POST /extract`: Extracts content directly from a list of provided URLs, using a JS-enabled headless browser for maximum compatibility with modern websites and single-page applications.
*   **Multi-Source Content Extraction**: Automatically detects and handles different content types:
*   **YouTube**: Extracts video title, channel name, and top comments. Full video transcripts are fetched via a dedicated, high-performance Python microservice.
*   **Reddit**: Fetches post title, body, and comments. Also supports extracting recent posts from subreddit and user profile URLs.
*   **PDFs**: Extracts clean text content from PDF documents.
    -   **Webpages**: Scrapes and cleans the main textual content from articles, blogs, and dynamic single-page applications.
*   **Flexible Search Backend**:
    *   Integrates with a self-hosted **SearxNG** instance or the **Serper.dev** Google Search API.
    *   Supports a primary and fallback search engine configuration.
*   **Performance Optimized**:
    *   **Concurrent Processing**: Extracts from multiple URLs in parallel using separate worker pools for I/O-bound and CPU-bound tasks.
    *   **Decoupled Architecture**: YouTube transcript extraction is handled by a separate, independent Python microservice, improving the main service's performance and stability.
    *   **Advanced Caching**: Supports sharded in-memory and Redis caching with batched operations for both search results and extracted content.
   *   **Monitoring**: Includes a `GET /health` endpoint for simple health checks.
   
## Try it Live

You can see this API in action by checking out the **Discord AI Chatbot**, which uses RAG-Forge as its primary tool for web content extraction.

*   **Discord AI Chatbot**: [`https://github.com/anojndr/Discord_AI_chatbot`](https://github.com/anojndr/Discord_AI_chatbot)

This provides a real-world example of how to integrate RAG-Forge into an application to power its knowledge-gathering capabilities.

## Project Structure

A high-level overview of the main directories and key files:

*   `internal/`: Contains the core Go application logic.
    *   `api/`: Handles API request routing, payload processing, caching, and worker dispatching.
    *   `extractor/`: Implements the content extraction logic for all supported source types, with different strategies for different endpoints.
    *   `searxng/`: Client for interacting with search engines (SearxNG and Serper).
    *   `worker/`: Manages the worker pools for concurrent job processing.
*   `transcript-service/`: A separate Python FastAPI microservice for YouTube transcript extraction.
*   [`main.go`](main.go): The entry point for the Go API server.
*   [`DOCS.md`](DOCS.md): **Comprehensive documentation on setup, configuration, API reference, and usage.**
*   [`go.mod`](go.mod): Defines the Go module and its dependencies.

## Prerequisites

To run this project, you need the following installed:

*   **Go**: Version 1.23.1 or higher.
*   **Python**: Version 3.8 or higher.
*   **External Tools**:
	*   **`pdftotext`**: For PDF extraction.
        - **Windows**: Download [Poppler binaries](https://github.com/oschwartz10612/poppler-windows/releases/), unzip, and add the `bin` folder to your system `PATH`.
        - **Linux**: `sudo apt-get install poppler-utils`
        - **macOS**: `brew install poppler`
    *   **Chromium-based browser**: For JS-enabled extraction of dynamic webpages.
*   **Search Engine**: A running **SearxNG** instance OR a **Serper API** key.

For detailed installation instructions, please refer to the **[Installation section in DOCS.md](DOCS.md)**.

## Quick Start

1.  **Clone the Repository:**
    ```bash
    git clone https://github.com/your-username/RAG-Forge.git
    cd RAG-Forge
    ```

2.  **Configure Your Environment:**
    Copy the example environment file and edit it with your own settings (API keys, URLs, credentials).
    ```bash
    cp ENV_EXAMPLE.TXT .env
    # On Windows, use: copy ENV_EXAMPLE.TXT .env
    nano .env
    ```

3.  **Run the Application:**
    Use the script for your operating system. It will set up the Python environment and run both services.
   
    **On Linux/macOS:**
    ```bash
    ./run.sh
    ```
    **On Windows:**
    ```cmd
    run.bat
    ```

## API Usage

Once running, the API is available at `http://127.0.0.1:8086`.

**Example: Search and extract content (fast mode)**
```bash
curl -X POST http://127.0.0.1:8086/search \
-H "Content-Type: application/json" \
-d '{
  "query": "benefits of learning Go",
  "max_results": 3
}'
```

<!-- Twitter/X extraction removed from project -->

## Complete Documentation

📖 **For comprehensive setup guides, API reference, configuration details, code examples, and troubleshooting, please see [DOCS.md](DOCS.md).**

## Development

Standard Go commands (`go build`, `go test`, `go run main.go`) are used for development.

## License

This project is licensed under the Apache License 2.0. See the [LICENSE.md](LICENSE.md) file for details.