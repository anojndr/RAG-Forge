package extractor

import (
	"bytes"
	"errors"
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
func (e *PDFExtractor) Extract(url string, endpoint string, maxChars *int) (*ExtractedResult, error) {
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

	// Add this check
	const maxPDFSize = 20 * 1024 * 1024 // 20 MB
	if resp.ContentLength > maxPDFSize {
		errMsg := fmt.Sprintf("PDF file size (%d bytes) exceeds the limit of %d bytes", resp.ContentLength, maxPDFSize)
		result.Error = errMsg
		logger.LogError("PDFExtractor: %s for %s", errMsg, url)
		return result, errors.New(errMsg)
	}

	// 2. Process the response body as a stream
	textContent, err := e.extractTextFromPDF(resp.Body, resp.ContentLength)
	if err != nil {
		// Check if the error is due to non-PDF content
		if err == ErrNotPDF {
			result.Error = ErrNotPDF.Error()
			return result, ErrNotPDF
		}
		errMsg := fmt.Sprintf("failed to extract text from PDF stream: %v", err)
		result.Error = errMsg
		logger.LogError("PDFExtractor: Error processing stream for %s: %v", url, err)
		return result, fmt.Errorf("pdf stream processing failed for %s: %w", url, err)
	}

	// 3. Truncate content if necessary
	if maxChars != nil && len(textContent) > *maxChars {
		textContent = textContent[:*maxChars]
		log.Printf("PDFExtractor: Truncated content to %d characters for %s", *maxChars, url)
	}

	log.Printf("PDFExtractor: Successfully extracted %d characters from %s", len(textContent), url)

	result.ProcessedSuccessfully = true
	result.Data = PDFData{
		TextContent: textContent,
	}
	return result, nil
}

// extractTextFromPDF extracts text from PDF content.
func (e *PDFExtractor) extractTextFromPDF(reader io.Reader, contentLength int64) (string, error) {
	// The pdf library requires an io.ReaderAt, so we have to buffer the whole thing in memory.
	// This is not ideal, but it's a limitation of the library.
	// We will at least check the content type first to avoid buffering non-PDF files.
	buf := new(bytes.Buffer)
	teeReader := io.TeeReader(reader, buf)

	// Read a small chunk to detect content type
	header := make([]byte, 512)
	n, err := io.ReadFull(teeReader, header)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return "", fmt.Errorf("failed to read header: %w", err)
	}
	header = header[:n]

	fileType := e.detectFileType(header)
	if fileType != "pdf" {
		return "", ErrNotPDF
	}

	// The rest of the stream is now in buf, continue reading the original reader
	remainingReader := io.MultiReader(buf, reader)

	// We still need to read the whole thing for the pdf library
	pdfBytes, err := io.ReadAll(remainingReader)
	if err != nil {
		return "", fmt.Errorf("failed to read full PDF body: %w", err)
	}

	r := bytes.NewReader(pdfBytes)
	pdfReader, err := pdf.NewReader(r, int64(len(pdfBytes)))
	if err != nil {
		return "", fmt.Errorf("failed to create PDF reader: %w", err)
	}

	var textBuf bytes.Buffer
	b, err := pdfReader.GetPlainText()
	if err != nil {
		return "", fmt.Errorf("failed to get plain text from PDF: %w", err)
	}

	_, err = textBuf.ReadFrom(b)
	if err != nil {
		return "", fmt.Errorf("failed to read text from buffer: %w", err)
	}

	return textBuf.String(), nil
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

