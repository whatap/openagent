package scraper

import (
	"fmt"
	"log"

	"open-agent/pkg/client"
	"open-agent/pkg/model"
)

// ScraperTask represents a task to scrape metrics from a target
type ScraperTask struct {
	JobName      string
	TargetURL    string
	FilterConfig map[string]interface{}
}

// NewScraperTask creates a new ScraperTask instance
func NewScraperTask(jobName, targetURL string, filterConfig map[string]interface{}) *ScraperTask {
	return &ScraperTask{
		JobName:      jobName,
		TargetURL:    targetURL,
		FilterConfig: filterConfig,
	}
}

// Run executes the scraper task
func (st *ScraperTask) Run() (*model.ScrapeRawData, error) {
	// Format the URL
	formattedURL := client.FormatURL(st.TargetURL)

	// Execute the HTTP request
	httpClient := client.GetInstance()
	response, err := httpClient.ExecuteGet(formattedURL)
	if err != nil {
		return nil, fmt.Errorf("error scraping target %s for job %s: %v", st.TargetURL, st.JobName, err)
	}

	// Create a ScrapeRawData instance with the response
	rawData := model.NewScrapeRawData(st.TargetURL, response, st.FilterConfig)

	log.Printf("ScraperTask: job [%s] fetched target %s (length=%d)", st.JobName, st.TargetURL, len(response))

	return rawData, nil
}
