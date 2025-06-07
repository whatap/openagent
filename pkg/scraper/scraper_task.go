package scraper

import (
	"fmt"
	"log"
	"strings"
	"time"

	"open-agent/pkg/client"
	"open-agent/pkg/config"
	"open-agent/pkg/model"
)

// Global WhatapConfig instance
var whatapConfig *config.WhatapConfig

func init() {
	// Initialize the WhatapConfig
	whatapConfig = config.NewWhatapConfig()
}

// ScraperTask represents a task to scrape metrics from a target
type ScraperTask struct {
	JobName              string
	TargetURL            string
	MetricRelabelConfigs model.RelabelConfigs
	TLSConfig            *client.TLSConfig
}

// NewScraperTask creates a new ScraperTask instance
func NewScraperTask(jobName, targetURL string, metricRelabelConfigs model.RelabelConfigs, tlsConfig *client.TLSConfig) *ScraperTask {
	return &ScraperTask{
		JobName:              jobName,
		TargetURL:            targetURL,
		MetricRelabelConfigs: metricRelabelConfigs,
		TLSConfig:            tlsConfig,
	}
}

// Run executes the scraper task
func (st *ScraperTask) Run() (*model.ScrapeRawData, error) {
	// Format the URL
	formattedURL := client.FormatURL(st.TargetURL)

	// Log detailed information if debug is enabled
	if whatapConfig.IsDebugEnabled() {
		log.Printf("[DEBUG] Starting scraper task for job [%s], target [%s]", st.JobName, st.TargetURL)
		log.Printf("[DEBUG] Formatted URL: %s", formattedURL)
		if st.TLSConfig != nil {
			log.Printf("[DEBUG] Using TLS config with InsecureSkipVerify=%v", st.TLSConfig.InsecureSkipVerify)
		}
		if len(st.MetricRelabelConfigs) > 0 {
			log.Printf("[DEBUG] Using %d metric relabel configs", len(st.MetricRelabelConfigs))
			for i, config := range st.MetricRelabelConfigs {
				log.Printf("[DEBUG] Relabel config #%d: Action=%s, SourceLabels=%v, TargetLabel=%s, Regex=%s", 
					i+1, config.Action, config.SourceLabels, config.TargetLabel, config.Regex)
			}
		}
	}

	// Record start time for performance measurement if debug is enabled
	var startTime time.Time
	if whatapConfig.IsDebugEnabled() {
		startTime = time.Now()
	}

	// Execute the HTTP request
	httpClient := client.GetInstance()
	var response string
	var err error

	if st.TLSConfig != nil {
		response, err = httpClient.ExecuteGetWithTLSConfig(formattedURL, st.TLSConfig)
	} else {
		response, err = httpClient.ExecuteGet(formattedURL)
	}

	if err != nil {
		if whatapConfig.IsDebugEnabled() {
			log.Printf("[DEBUG] Error scraping target %s for job %s: %v", st.TargetURL, st.JobName, err)
		}
		return nil, fmt.Errorf("error scraping target %s for job %s: %v", st.TargetURL, st.JobName, err)
	}

	// Create a ScrapeRawData instance with the response
	rawData := model.NewScrapeRawData(st.TargetURL, response, st.MetricRelabelConfigs)

	// Log detailed information if debug is enabled
	if whatapConfig.IsDebugEnabled() {
		duration := time.Since(startTime)
		log.Printf("[DEBUG] Scraper task completed for job [%s], target [%s] in %v", st.JobName, st.TargetURL, duration)
		log.Printf("[DEBUG] Response length: %d bytes", len(response))

		// Log a preview of the response (first 500 characters)
		preview := response
		if len(preview) > 500 {
			preview = preview[:500] + "..."
		}
		log.Printf("[DEBUG] Response preview: %s", preview)

		// Count the number of metrics in the response (approximate)
		metricCount := 0
		for _, line := range strings.Split(response, "\n") {
			// Skip empty lines, comments, and metadata lines
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			metricCount++
		}
		log.Printf("[DEBUG] Approximate number of metrics: %d", metricCount)
	}

	log.Printf("ScraperTask: job [%s] fetched target %s (length=%d)", st.JobName, st.TargetURL, len(response))

	return rawData, nil
}
