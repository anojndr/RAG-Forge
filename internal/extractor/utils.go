package extractor

// truncateText truncates a string to a specified maximum length.
func truncateText(s string, max int) string {
	if len(s) > max {
		return s[:max]
	}
	return s
}