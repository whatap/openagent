package model

// ScrapeRawData represents raw metrics data scraped from a target
type ScrapeRawData struct {
	TargetURL           string
	RawData             string
	MetricRelabelConfigs RelabelConfigs
}

// NewScrapeRawData creates a new ScrapeRawData instance
func NewScrapeRawData(targetURL, rawData string, metricRelabelConfigs RelabelConfigs) *ScrapeRawData {
	return &ScrapeRawData{
		TargetURL:           targetURL,
		RawData:             rawData,
		MetricRelabelConfigs: metricRelabelConfigs,
	}
}

// NewScrapeRawDataFromFilterConfig creates a new ScrapeRawData instance from a filterConfig map
// This is kept for backward compatibility
func NewScrapeRawDataFromFilterConfig(targetURL, rawData string, filterConfig map[string]interface{}) *ScrapeRawData {
	// Extract metricRelabelConfigs from filterConfig if present
	var metricRelabelConfigs RelabelConfigs
	if filterConfig != nil {
		if relabelConfigs, ok := filterConfig["metricRelabelConfigs"].([]interface{}); ok {
			metricRelabelConfigs = ParseRelabelConfigs(relabelConfigs)
		}
	}

	return &ScrapeRawData{
		TargetURL:           targetURL,
		RawData:             rawData,
		MetricRelabelConfigs: metricRelabelConfigs,
	}
}
