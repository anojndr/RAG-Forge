package extractor

import "errors"

// ErrNotPDF is returned when content sniffed is not a valid PDF.
var ErrNotPDF = errors.New("content is not a valid PDF")

// ErrUnsupportedContentType is returned when the content type is not supported for extraction.
var ErrUnsupportedContentType = errors.New("unsupported content type")