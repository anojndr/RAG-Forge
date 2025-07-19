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
		err := resp.Body.Close()
		if err != nil {
			logger.LogError("PDFExtractor: failed to close response body for %s: %v", url, err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		errMsg := fmt.Sprintf("failed to download PDF, status: %s", resp.Status)
		result.Error = errMsg
		logger.LogError("PDFExtractor: Error downloading %s, status: %s", url, resp.Status)
		return result, fmt.Errorf("pdf download failed for %s with status %s", url, resp.Status)
	}

	// 2. Extract text directly from the response body
	textContent, err := e.extractTextFromReader(resp.Body, resp.ContentLength)
	if err != nil {
		result.Error = err.Error()
		// Check if it was an HTML response and try to extract from it
		if strings.Contains(err.Error(), "unsupported file type: html") {
			log.Printf("PDFExtractor: File appears to be HTML, attempting HTML extraction for %s", url)
			// We need to re-download the file as the body has been consumed
			resp, err := e.HTTPClient.Do(req)
			if err != nil {
				return result, fmt.Errorf("failed to re-download for HTML extraction: %w", err)
			}
			defer func() {
				err := resp.Body.Close()
				if err != nil {
					logger.LogError("PDFExtractor: failed to close response body for %s: %v", url, err)
				}
			}()
			htmlContent, readErr := io.ReadAll(resp.Body)
			if readErr != nil {
				return result, fmt.Errorf("failed to read HTML body: %w", readErr)
			}
			textContent, htmlErr := e.extractFromHTML(string(htmlContent), url)
			if htmlErr != nil {
				result.Error = htmlErr.Error()
				return result, htmlErr
			}
			result.Data = PDFData{TextContent: textContent}
			result.ProcessedSuccessfully = true
			return result, nil
		}
		return result, err
	}

	log.Printf("PDFExtractor: Successfully extracted %d characters from %s", len(textContent), url)

	result.ProcessedSuccessfully = true
	result.Data = PDFData{
		TextContent: textContent,
	}
	return result, nil
}

// extractTextFromReader extracts text from a PDF using an io.Reader.
func (e *PDFExtractor) extractTextFromReader(reader io.Reader, size int64) (string, error) {
	// The dslipak/pdf library requires an io.ReaderAt, so we need to buffer the whole PDF in memory.
	// This is a limitation of the library.
	pdfBytes, err := io.ReadAll(reader)
	if err != nil {
		return "", fmt.Errorf("failed to read PDF body: %w", err)
	}

	// Detect file type from the buffered bytes
	fileType := e.detectFileType(pdfBytes)
	log.Printf("PDFExtractor: Detected file type: %s", fileType)

	if fileType != "pdf" {
		return "", fmt.Errorf("unsupported file type: %s", fileType)
	}

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
