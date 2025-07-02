package model

// ScrapeRawData represents raw metrics data scraped from a target
type ScrapeRawData struct {
	TargetURL            string
	RawData              string
	MetricRelabelConfigs RelabelConfigs
	NodeName             string
	AddNodeLabel         bool
	CollectionTime       int64 // Unix timestamp in milliseconds when data was collected
}

// NewScrapeRawData creates a new ScrapeRawData instance
func NewScrapeRawData(targetURL, rawData string, metricRelabelConfigs RelabelConfigs, collectionTime int64) *ScrapeRawData {
	return &ScrapeRawData{
		TargetURL:            targetURL,
		RawData:              rawData,
		MetricRelabelConfigs: metricRelabelConfigs,
		NodeName:             "",
		AddNodeLabel:         false,
		CollectionTime:       collectionTime,
	}
}

// NewScrapeRawDataWithNodeName creates a new ScrapeRawData instance with node name
func NewScrapeRawDataWithNodeName(targetURL, rawData string, metricRelabelConfigs RelabelConfigs, nodeName string, addNodeLabel bool, collectionTime int64) *ScrapeRawData {
	return &ScrapeRawData{
		TargetURL:            targetURL,
		RawData:              rawData,
		MetricRelabelConfigs: metricRelabelConfigs,
		NodeName:             nodeName,
		AddNodeLabel:         addNodeLabel,
		CollectionTime:       collectionTime,
	}
}
