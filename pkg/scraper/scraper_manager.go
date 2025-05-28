package scraper

import (
	"log"
	"time"

	"open-agent/pkg/config"
	"open-agent/pkg/model"
)

// ScraperManager is responsible for managing scraper tasks
type ScraperManager struct {
	configManager *config.ConfigManager
	rawQueue      chan *model.ScrapeRawData
}

// NewScraperManager creates a new ScraperManager instance
func NewScraperManager(configManager *config.ConfigManager, rawQueue chan *model.ScrapeRawData) *ScraperManager {
	return &ScraperManager{
		configManager: configManager,
		rawQueue:      rawQueue,
	}
}

// StartScraping starts the scraping process
func (sm *ScraperManager) StartScraping() {
	// Get the configuration
	config := sm.configManager.GetConfig()
	if config == nil {
		log.Println("No configuration loaded.")
		return
	}

	// Get the scrape interval
	scrapeIntervalStr := sm.configManager.GetScrapeInterval()
	scrapeIntervalSeconds, err := sm.configManager.ParseInterval(scrapeIntervalStr)
	if err != nil {
		log.Printf("Error parsing scrape interval: %v. Using default of 15 seconds.", err)
		scrapeIntervalSeconds = 15
	}

	// Get the scrape configs
	scrapeConfigs := sm.configManager.GetScrapeConfigs()
	if scrapeConfigs == nil {
		log.Println("No scrape_configs found in configuration.")
		return
	}

	// Schedule scraper tasks for each target
	for _, scrapeConfig := range scrapeConfigs {
		jobName, ok := scrapeConfig["job_name"].(string)
		if !ok {
			continue
		}

		staticConfig, ok := scrapeConfig["static_config"].(map[string]interface{})
		if !ok {
			continue
		}

		targets, ok := staticConfig["targets"].([]interface{})
		if !ok {
			continue
		}

		var filterConfig map[string]interface{}
		if filter, ok := staticConfig["filter"].(map[string]interface{}); ok {
			filterConfig = filter
		}

		// Schedule a scraper task for each target
		for _, target := range targets {
			targetStr, ok := target.(string)
			if !ok {
				continue
			}

			go sm.scheduleScraper(jobName, targetStr, filterConfig, time.Duration(scrapeIntervalSeconds)*time.Second)
		}
	}
}

// scheduleScraper schedules a scraper task to run at regular intervals
func (sm *ScraperManager) scheduleScraper(jobName, target string, filterConfig map[string]interface{}, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Create a scraper task
	scraperTask := NewScraperTask(jobName, target, filterConfig)

	// Run the scraper task immediately
	sm.runScraperTask(scraperTask)

	// Run the scraper task at regular intervals
	for range ticker.C {
		sm.runScraperTask(scraperTask)
	}
}

// runScraperTask runs a scraper task and adds the result to the raw queue
func (sm *ScraperManager) runScraperTask(scraperTask *ScraperTask) {
	rawData, err := scraperTask.Run()
	if err != nil {
		log.Printf("Error running scraper task: %v", err)
		return
	}

	// Add the raw data to the queue
	sm.rawQueue <- rawData
}

// AddRawData adds raw data to the queue
func (sm *ScraperManager) AddRawData(data *model.ScrapeRawData) {
	sm.rawQueue <- data
}
