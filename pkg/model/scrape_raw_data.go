package model

// ScrapeRawData represents raw metrics data scraped from a target
type ScrapeRawData struct {
	TargetURL            string
	RawData              string
	MetricRelabelConfigs RelabelConfigs
	Labels               map[string]string // Target labels
	NodeName             string
	AddNodeLabel         bool
	CollectionTime       int64 // Unix timestamp in milliseconds when data was collected
}

// NewScrapeRawData creates a new ScrapeRawData instance
func NewScrapeRawData(targetURL, rawData string, metricRelabelConfigs RelabelConfigs, labels map[string]string, collectionTime int64) *ScrapeRawData {
	return &ScrapeRawData{
		TargetURL:            targetURL,
		RawData:              rawData,
		MetricRelabelConfigs: metricRelabelConfigs,
		Labels:               labels,
		NodeName:             "",
		AddNodeLabel:         false,
		CollectionTime:       collectionTime,
	}
}

// NewScrapeRawDataWithNodeName creates a new ScrapeRawData instance with node name
func NewScrapeRawDataWithNodeName(targetURL, rawData string, metricRelabelConfigs RelabelConfigs, labels map[string]string, nodeName string, addNodeLabel bool, collectionTime int64) *ScrapeRawData {
	return &ScrapeRawData{
		TargetURL:            targetURL,
		RawData:              rawData,
		MetricRelabelConfigs: metricRelabelConfigs,
		Labels:               labels,
		NodeName:             nodeName,
		AddNodeLabel:         addNodeLabel,
		CollectionTime:       collectionTime,
	}
}
