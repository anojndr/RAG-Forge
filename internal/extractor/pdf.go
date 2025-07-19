package extractor

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/dslipak/pdf"
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

// Extract downloads a PDF from a URL and extracts its text content using a native Go library.
func (e *PDFExtractor) Extract(url string) (*ExtractedResult, error) {
	log.Printf("PDFExtractor: Starting extraction for URL: %s", url)
	result := &ExtractedResult{
		URL:        url,
		SourceType: "pdf",
	}

	// 1. Download the content
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
		errMsg := fmt.Sprintf("failed to download content: %v", err)
		result.Error = errMsg
		logger.LogError("PDFExtractor: Error downloading %s: %v", url, err)
		return result, fmt.Errorf("download failed for %s: %w", url, err)
	}
	defer func() {
		err := resp.Body.Close()
		if err != nil {
			logger.LogError("PDFExtractor: failed to close response body for %s: %v", url, err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		errMsg := fmt.Sprintf("failed to download content, status: %s", resp.Status)
		result.Error = errMsg
		logger.LogError("PDFExtractor: Error downloading %s, status: %s", url, resp.Status)
		return result, fmt.Errorf("download failed for %s with status %s", url, resp.Status)
	}

	// 2. Buffer the entire response body to avoid re-downloading
	content, err := io.ReadAll(resp.Body)
	if err != nil {
		errMsg := fmt.Sprintf("failed to read response body: %v", err)
		result.Error = errMsg
		logger.LogError("PDFExtractor: Error reading body for %s: %v", url, err)
		return result, fmt.Errorf("body read failed for %s: %w", url, err)
	}

	// 3. Detect content type and extract accordingly
	fileType := e.detectFileType(content)
	log.Printf("PDFExtractor: Detected file type '%s' for URL: %s", fileType, url)

	var textContent string
	var extractionErr error

	switch fileType {
	case "pdf":
		textContent, extractionErr = e.extractTextFromPDF(content)
		if extractionErr != nil {
			result.Error = extractionErr.Error()
			return result, extractionErr
		}
	case "html":
		// Return a specific error to indicate that the content is HTML, not PDF.
		// The dispatcher will use this to fall back to the WebpageExtractor.
		result.Error = ErrNotPDF.Error()
		return result, ErrNotPDF
	default:
		err := fmt.Errorf("unsupported file type: %s", fileType)
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

// extractTextFromPDF extracts text from PDF content.
func (e *PDFExtractor) extractTextFromPDF(pdfBytes []byte) (string, error) {
	r := bytes.NewReader(pdfBytes)
	pdfReader, err := pdf.NewReader(r, int64(len(pdfBytes)))
	if err != nil {
		return "", fmt.Errorf("failed to create PDF reader: %w", err)
	}

	var buf bytes.Buffer
	b, err := pdfReader.GetPlainText()
	if err != nil {
		return "", fmt.Errorf("failed to get plain text from PDF: %w", err)
	}

	_, err = buf.ReadFrom(b)
	if err != nil {
		return "", fmt.Errorf("failed to read text from buffer: %w", err)
	}

	return buf.String(), nil
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

// extractFromHTML extracts text from HTML content when PDF URL returns HTML
func (e *PDFExtractor) extractFromHTML(htmlContent, url string) (string, error) {
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
