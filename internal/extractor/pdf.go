package extractor

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"web-search-api-for-llms/internal/config"
	"web-search-api-for-llms/internal/logger"
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

// Extract downloads a PDF from a URL, saves it temporarily,
// and uses pdftotext to extract its text content.
func (e *PDFExtractor) Extract(url string) (*ExtractedResult, error) {
	log.Printf("PDFExtractor: Starting extraction for URL: %s", url)
	result := &ExtractedResult{
		URL:        url,
		SourceType: "pdf",
	}

	// 1. Download the PDF
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		errMsg := fmt.Sprintf("failed to create request: %v", err)
		result.Error = errMsg
		logger.LogError("PDFExtractor: Error creating request for %s: %v", url, err)
		return result, fmt.Errorf("pdf request creation failed for %s: %w", url, err)
	}
	req.Header.Set("User-Agent", useragent.Random())

	resp, err := e.HTTPClient.Do(req)
	if err != nil {
		errMsg := fmt.Sprintf("failed to download PDF: %v", err)
		result.Error = errMsg
		logger.LogError("PDFExtractor: Error downloading %s: %v", url, err)
		return result, fmt.Errorf("pdf download failed for %s: %w", url, err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Printf("PDFExtractor: Warning - failed to close response body: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		errMsg := fmt.Sprintf("failed to download PDF, status: %s", resp.Status)
		result.Error = errMsg
		logger.LogError("PDFExtractor: Error downloading %s, status: %s", url, resp.Status)
		return result, fmt.Errorf("pdf download failed for %s with status %s", url, resp.Status)
	}

	// 2. Create temporary file and stream PDF content
	tempDir := filepath.Join(os.TempDir(), "pdf_extractor_temp")
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		errMsg := fmt.Sprintf("failed to create temp directory: %v", err)
		result.Error = errMsg
		logger.LogError("PDFExtractor: Error creating temp directory %s: %v", tempDir, err)
		return result, fmt.Errorf("temp directory creation failed: %w", err)
	}

	tempFile, err := os.CreateTemp(tempDir, "downloaded-*.pdf")
	if err != nil {
		errMsg := fmt.Sprintf("failed to create temp file: %v", err)
		result.Error = errMsg
		logger.LogError("PDFExtractor: Error creating temp file: %v", err)
		return result, fmt.Errorf("temp file creation failed: %w", err)
	}

	tempFilePath := tempFile.Name()
	defer func() {
		if err := os.Remove(tempFilePath); err != nil {
			log.Printf("PDFExtractor: Warning - failed to clean up temp file %s: %v", tempFilePath, err)
		}
	}()

	// Stream response body to the file to avoid holding the entire PDF in memory
	bytesWritten, err := io.Copy(tempFile, resp.Body)
	if err != nil {
		if closeErr := tempFile.Close(); closeErr != nil {
			log.Printf("PDFExtractor: Warning - failed to close temp file during error handling: %v", closeErr)
		}
		errMsg := fmt.Sprintf("failed to write PDF to temp file: %v", err)
		result.Error = errMsg
		logger.LogError("PDFExtractor: Error streaming PDF to temp file %s: %v", tempFilePath, err)
		return result, fmt.Errorf("pdf streaming failed: %w", err)
	}

	if err := tempFile.Close(); err != nil {
		errMsg := fmt.Sprintf("failed to close temp file: %v", err)
		result.Error = errMsg
		logger.LogError("PDFExtractor: Error closing temp file %s: %v", tempFilePath, err)
		return result, fmt.Errorf("temp file close failed: %w", err)
	}

	log.Printf("PDFExtractor: Successfully downloaded and saved %d bytes to %s", bytesWritten, tempFilePath)

	// 3. Verify file type and extract text concurrently with multiple methods
	textContent, err := e.extractTextFromFile(tempFilePath, url)
	if err != nil {
		result.Error = err.Error()
		return result, err
	}
	log.Printf("PDFExtractor: Successfully extracted %d characters from %s", len(textContent), url)

	result.ProcessedSuccessfully = true
	result.Data = PDFData{
		TextContent: textContent,
	}
	return result, nil
}

// extractTextFromFile detects file type and extracts text using appropriate method
func (e *PDFExtractor) extractTextFromFile(filePath, url string) (string, error) {
	// First, detect if this is actually a PDF file
	fileType, err := e.detectFileType(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to detect file type: %w", err)
	}

	log.Printf("PDFExtractor: Detected file type: %s for %s", fileType, url)

	switch fileType {
	case "pdf":
		return e.extractWithPdftotext(filePath, url)
	case "html":
		log.Printf("PDFExtractor: File appears to be HTML, attempting HTML extraction for %s", url)
		return e.extractFromHTML(filePath, url)
	default:
		return "", fmt.Errorf("unsupported file type: %s (expected PDF but got %s)", fileType, fileType)
	}
}

// detectFileType examines file header to determine actual file type
func (e *PDFExtractor) detectFileType(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open file for type detection: %w", err)
	}
	defer func() {
		if cerr := file.Close(); cerr != nil {
			log.Printf("PDFExtractor: Warning - failed to close file after type detection: %v", cerr)
		}
	}()

	// Read first 512 bytes to detect file type
	header := make([]byte, 512)
	n, err := file.Read(header)
	if err != nil && err != io.EOF {
		return "", fmt.Errorf("failed to read file header: %w", err)
	}

	headerStr := string(header[:n])
	headerLower := strings.ToLower(headerStr)

	// Check for PDF signature
	if strings.HasPrefix(headerStr, "%PDF-") {
		return "pdf", nil
	}

	// Check for HTML indicators
	if strings.Contains(headerLower, "<html") ||
		strings.Contains(headerLower, "<!doctype html") ||
		strings.Contains(headerLower, "<head>") ||
		strings.Contains(headerLower, "<title>") {
		return "html", nil
	}

	// Check for common binary signatures
	if len(header) >= 4 {
		// Check for other common file types that might be misidentified as PDF
		if bytes.HasPrefix(header, []byte{0x50, 0x4B, 0x03, 0x04}) { // ZIP
			return "zip", nil
		}
		if bytes.HasPrefix(header, []byte{0x89, 0x50, 0x4E, 0x47}) { // PNG
			return "png", nil
		}
		if bytes.HasPrefix(header, []byte{0xFF, 0xD8, 0xFF}) { // JPEG
			return "jpeg", nil
		}
	}

	return "unknown", nil
}

// extractWithPdftotext uses pdftotext command with concurrent extraction attempts
func (e *PDFExtractor) extractWithPdftotext(filePath, url string) (string, error) {
	type extractResult struct {
		text   string
		method string
		err    error
	}

	resultsChan := make(chan extractResult, 3)
	var wg sync.WaitGroup

	// Try standard pdftotext first
	wg.Add(1)
	go func() {
		defer wg.Done()
		cmd := exec.Command("pdftotext", filePath, "-")
		var out bytes.Buffer
		var stderr bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &stderr

		if err := cmd.Run(); err != nil {
			stderrStr := stderr.String()
			logger.LogError("PDFExtractor: pdftotext standard error for %s: %s", filePath, stderrStr)
			resultsChan <- extractResult{method: "standard", err: fmt.Errorf("standard pdftotext failed: %s", stderrStr)}
			return
		}

		textContent := out.String()
		if strings.TrimSpace(textContent) != "" {
			resultsChan <- extractResult{text: textContent, method: "standard", err: nil}
			return
		}
		resultsChan <- extractResult{method: "standard", err: fmt.Errorf("standard pdftotext returned empty content")}
	}()

	// Try -raw option concurrently
	wg.Add(1)
	go func() {
		defer wg.Done()
		cmd := exec.Command("pdftotext", "-raw", filePath, "-")
		var out bytes.Buffer
		var stderr bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &stderr

		if err := cmd.Run(); err != nil {
			resultsChan <- extractResult{method: "raw", err: fmt.Errorf("raw option failed")}
			return
		}

		textContent := out.String()
		if strings.TrimSpace(textContent) != "" {
			resultsChan <- extractResult{text: textContent, method: "raw", err: nil}
			return
		}
		resultsChan <- extractResult{method: "raw", err: fmt.Errorf("raw option returned empty content")}
	}()

	// Try -layout option concurrently
	wg.Add(1)
	go func() {
		defer wg.Done()
		cmd := exec.Command("pdftotext", "-layout", filePath, "-")
		var out bytes.Buffer
		var stderr bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &stderr

		if err := cmd.Run(); err != nil {
			resultsChan <- extractResult{method: "layout", err: fmt.Errorf("layout option failed")}
			return
		}

		textContent := out.String()
		if strings.TrimSpace(textContent) != "" {
			resultsChan <- extractResult{text: textContent, method: "layout", err: nil}
			return
		}
		resultsChan <- extractResult{method: "layout", err: fmt.Errorf("layout option returned empty content")}
	}()

	// Wait for all goroutines to complete
	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	// Return the first successful result, preferring standard method
	var results []extractResult
	for result := range resultsChan {
		results = append(results, result)
		if result.err == nil && result.text != "" {
			log.Printf("PDFExtractor: Successfully extracted text using %s method for %s", result.method, url)
			return result.text, nil
		}
	}

	// If all methods failed, return the first error
	if len(results) > 0 {
		return "", fmt.Errorf("all PDF extraction methods failed for %s: %v", url, results[0].err)
	}

	return "", fmt.Errorf("no PDF extraction methods completed for %s", url)
}

// extractFromHTML extracts text from HTML content when PDF URL returns HTML
func (e *PDFExtractor) extractFromHTML(filePath, url string) (string, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read HTML file: %w", err)
	}

	htmlContent := string(content)

	// Basic HTML text extraction - remove tags and decode entities
	text := e.stripHTMLTags(htmlContent)

	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("no text content found in HTML response from %s", url)
	}

	return text, nil
}

// stripHTMLTags performs basic HTML tag removal
func (e *PDFExtractor) stripHTMLTags(html string) string {
	// Remove script and style content
	html = e.removeTagContent(html, "script")
	html = e.removeTagContent(html, "style")

	// Remove HTML tags
	result := ""
	inTag := false
	for _, char := range html {
		if char == '<' {
			inTag = true
		} else if char == '>' {
			inTag = false
		} else if !inTag {
			result += string(char)
		}
	}

	// Clean up whitespace
	lines := strings.Split(result, "\n")
	var cleanLines []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			cleanLines = append(cleanLines, line)
		}
	}

	return strings.Join(cleanLines, "\n")
}

// removeTagContent removes content between specified tags
func (e *PDFExtractor) removeTagContent(html, tagName string) string {
	startTag := "<" + tagName
	endTag := "</" + tagName + ">"

	for {
		start := strings.Index(strings.ToLower(html), startTag)
		if start == -1 {
			break
		}

		end := strings.Index(strings.ToLower(html[start:]), endTag)
		if end == -1 {
			break
		}

		end += start + len(endTag)
		html = html[:start] + html[end:]
	}

	return html
}
