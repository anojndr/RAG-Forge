# RAG-Forge: Web Content Extraction API

## Overview

RAG-Forge is a powerful, self-hosted Go API designed to fetch and extract clean, structured content from various web sources. It's built to be a reliable data-gathering tool for RAG (Retrieval-Augmented Generation) pipelines and other applications that need to consume web content.

The API supports two main extraction modes:
- **Search-based extraction**: Give it a search query, and it uses a configured search engine (SearxNG or Serper) to find relevant URLs and then extracts their content.
- **Direct URL extraction**: Provide a list of specific URLs, and it extracts the content directly.

The service is built with performance in mind, featuring concurrent processing of multiple URLs for high throughput and reliability.

## Key Features

*   **Dual Extraction Modes**:
    *   `POST /search`: Searches the web and extracts content from the top results.
    *   `POST /extract`: Extracts content directly from a list of provided URLs.
*   **Multi-Source Content Extraction**: Automatically detects and handles different content types:
    *   **Twitter/X**: Extracts full post content and comments via browser automation, bypassing public API limitations.
    *   **YouTube**: Extracts video title, channel name, top comments, and full transcript.
    *   **Reddit**: Fetches post title, body, score, author, and actual comment content (filtering out "load more" placeholders).
    *   **PDFs**: Extracts clean text content from PDF documents.
    *   **Webpages**: Scrapes and cleans the main textual content and title from articles and blogs.
*   **Flexible Search Backend**:
    *   Integrates with a self-hosted **SearxNG** instance or the **Serper.dev** Google Search API.
    *   Supports a primary and fallback search engine configuration.
*   **Performance Optimized**:
    *   **Concurrent Processing**: Extracts from multiple URLs in parallel for high throughput.
*   **Simplified Dependencies**: Automatically manages required Python packages in a local `venv` virtual environment.
*   **Monitoring**: Includes a `GET /health` endpoint for simple health checks.

## Try it Live

You can see this API in action by checking out the **Discord AI Chatbot**, which uses RAG-Forge as its primary tool for web content extraction.

*   **Discord AI Chatbot**: [`https://github.com/anojndr/Discord_AI_chatbot`](https://github.com/anojndr/Discord_AI_chatbot)

This provides a real-world example of how to integrate RAG-Forge into an application to power its knowledge-gathering capabilities.

## Project Structure

A high-level overview of the main directories and key files:

*   `internal/`: Contains the core Go application logic.
    *   `api/`: Handles API request routing, payload processing, and caching.
    *   `config/`: Manages application configuration from `.env` files.
    *   `extractor/`: Implements the content extraction logic for YouTube, Reddit, Twitter, PDFs, and webpages.
    *   `searxng/`: Client for interacting with search engines (SearxNG and Serper).
*   [`main.go`](main.go): The entry point for the Go API server.
*   [`DOCS.md`](DOCS.md): **Comprehensive documentation on setup, configuration, API reference, and usage.**
*   [`go.mod`](go.mod): Defines the Go module and its dependencies.

## Prerequisites

To run this project, you need the following installed and available in your system's PATH:

*   **Go**: Version 1.23.1 or higher.
*   **Python**: Version 3.8 or higher, along with `pip`.
*   **External Tools**:
    *   **`pdftotext`**: For PDF extraction (from the `poppler-utils` package).
    *   **Chromium-based browser**: For Twitter/X extraction (e.g., Google Chrome, Chromium).
*   **Search Engine**:
    *   A running **SearxNG** instance OR a **Serper API** key.

For detailed, command-line installation instructions, please refer to the **[Installation section in DOCS.md](DOCS.md)**.

## Quick Start

1.  **Clone the Repository:**
    ```bash
    git clone https://github.com/your-username/RAG-Forge.git
    cd RAG-Forge
    ```

2.  **Install System Dependencies:**
    Follow the detailed installation guide in **[DOCS.md](DOCS.md)** to install `poppler-utils` and a browser.

3.  **Configure Your Environment:**
    Copy the example environment file and edit it with your own settings (API keys, URLs, credentials).
    ```bash
    cp ENV_EXAMPLE.TXT .env
    nano .env
    ```

4.  **Run the API Server:**
    The server will automatically create a Python virtual environment and install necessary packages on first run.
    ```bash
    go run main.go
    ```

## API Usage

Once running, you can interact with the API via its endpoints.

**Example: Search and extract content**
```bash
curl -X POST http://localhost:8080/search \
-H "Content-Type: application/json" \
-d '{
  "query": "benefits of learning Go",
  "max_results": 3
}'
```

**Example: Extract content from a Twitter/X URL**
```bash
curl -X POST http://localhost:8080/extract \
-H "Content-Type: application/json" \
-d '{
  "urls": [
    "https://x.com/gvanrossum/status/1798372418833441227"
  ]
}'
```

## Complete Documentation

📖 **For comprehensive setup guides, API reference, configuration details, code examples, and troubleshooting, please see [DOCS.md](DOCS.md).**

## Development

Standard Go commands (`go build`, `go test`) are used for development. The Makefile has been removed to simplify the toolchain.

## License

This project is licensed under the Apache License 2.0. See the [LICENSE.md](LICENSE.md) file for details.