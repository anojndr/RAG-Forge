package extractor

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os/exec"
	"strings"

	"web-search-api-for-llms/internal/config"
	"web-search-api-for-llms/internal/useragent"
)

// PDFExtractor implements the Extractor interface for PDF files.
type PDFExtractor struct {
	BaseExtractor // Embed BaseExtractor for config and http client access
}

// NewPDFExtractor creates a new PDFExtractor.
func NewPDFExtractor(appConfig *config.AppConfig, client *http.Client) *PDFExtractor {
	return &PDFExtractor{
		BaseExtractor: NewBaseExtractor(appConfig, client),
	}
}

// Extract downloads a PDF from a URL and extracts its text content using a native Go library.
func (e *PDFExtractor) Extract(url string, endpoint string, maxChars *int, result *ExtractedResult) error {
	slog.Info("PDFExtractor: Starting extraction", "url", url)
	result.SourceType = "pdf"

	// 1. Download the content
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request for %s: %w", url, err)
	}
	req.Header.Set("User-Agent", useragent.Random())

	resp, err := e.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to download content from %s: %w", url, err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			slog.Warn("Failed to close response body", "url", url, "error", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed for %s with status %s", url, resp.Status)
	}

	// Add this check
	const maxPDFSize = 20 * 1024 * 1024 // 20 MB
	if resp.ContentLength > maxPDFSize {
		return fmt.Errorf("PDF file size (%d bytes) exceeds the limit of %d bytes", resp.ContentLength, maxPDFSize)
	}

	// 2. Process the response body as a stream
	textContent, err := e.extractTextFromPDF(resp.Body)
	if err != nil {
		// Check if the error is due to non-PDF content
		if err == ErrNotPDF {
			return ErrNotPDF
		}
		return fmt.Errorf("pdf stream processing failed for %s: %w", url, err)
	}

	// 3. Truncate content if necessary
	if maxChars != nil && len(textContent) > *maxChars {
		textContent = textContent[:*maxChars]
		slog.Info("PDFExtractor: Truncated content", "chars", *maxChars, "url", url)
	}

	slog.Info("PDFExtractor: Successfully extracted content", "chars", len(textContent), "url", url)

	result.ProcessedSuccessfully = true
	result.Data = PDFData{
		TextContent: textContent,
	}
	return nil
}

// extractTextFromPDF extracts text from PDF content using the pdftotext CLI tool.
func (e *PDFExtractor) extractTextFromPDF(reader io.Reader) (string, error) {
	// First, check the file type to ensure we're dealing with a PDF.
	header := make([]byte, 512)
	n, err := io.ReadFull(reader, header)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return "", fmt.Errorf("failed to read header: %w", err)
	}

	// Combine the header with the rest of the reader for the CLI tool.
	combinedReader := io.MultiReader(bytes.NewReader(header[:n]), reader)

	if e.detectFileType(header[:n]) != "pdf" {
		return "", ErrNotPDF
	}

	return e.extractTextFromPDFCLI(combinedReader)
}

// extractTextFromPDFCLI calls the `pdftotext` command-line tool.
func (e *PDFExtractor) extractTextFromPDFCLI(reader io.Reader) (string, error) {
	cmd := exec.Command("pdftotext", "-", "-") // Read from stdin, write to stdout
	cmd.Stdin = reader

	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("pdftotext failed: %s, err: %w", stderr.String(), err)
	}

	return out.String(), nil
}

// detectFileType examines file header to determine actual file type
func (e *PDFExtractor) detectFileType(data []byte) string {
	headerStr := string(data)
	headerLower := strings.ToLower(headerStr)

	// Check for PDF signature
	if strings.HasPrefix(headerStr, "%PDF-") {
		return "pdf"
	}

	// Check for HTML indicators
	if strings.Contains(headerLower, "<html") ||
		strings.Contains(headerLower, "<!doctype html") ||
		strings.Contains(headerLower, "<head>") ||
		strings.Contains(headerLower, "<title>") {
		return "html"
	}

	// Check for common binary signatures
	if len(data) >= 4 {
		// Check for other common file types that might be misidentified as PDF
		if bytes.HasPrefix(data, []byte{0x50, 0x4B, 0x03, 0x04}) { // ZIP
			return "zip"
		}
		if bytes.HasPrefix(data, []byte{0x89, 0x50, 0x4E, 0x47}) { // PNG
			return "png"
		}
		if bytes.HasPrefix(data, []byte{0xFF, 0xD8, 0xFF}) { // JPEG
			return "jpeg"
		}
	}

	return "unknown"
}
