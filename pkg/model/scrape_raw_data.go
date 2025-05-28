package model

// ScrapeRawData represents raw metrics data scraped from a target
type ScrapeRawData struct {
	TargetURL    string
	RawData      string
	FilterConfig map[string]interface{}
}

// NewScrapeRawData creates a new ScrapeRawData instance
func NewScrapeRawData(targetURL, rawData string, filterConfig map[string]interface{}) *ScrapeRawData {
	return &ScrapeRawData{
		TargetURL:    targetURL,
		RawData:      rawData,
		FilterConfig: filterConfig,
	}
}