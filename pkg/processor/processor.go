package processor

import (
	"encoding/json"
	"fmt"
	"log"

	"open-agent/pkg/config"
	"open-agent/pkg/converter"
	"open-agent/pkg/model"
)

// Global WhatapConfig instance
var whatapConfig *config.WhatapConfig

func init() {
	// Initialize the WhatapConfig
	whatapConfig = config.NewWhatapConfig()
}

// Processor is responsible for processing scraped metrics
type Processor struct {
	rawQueue        chan *model.ScrapeRawData
	processedQueue  chan *model.ConversionResult
}

// NewProcessor creates a new Processor instance
func NewProcessor(rawQueue chan *model.ScrapeRawData, processedQueue chan *model.ConversionResult) *Processor {
	return &Processor{
		rawQueue:       rawQueue,
		processedQueue: processedQueue,
	}
}

// Start starts the processor
func (p *Processor) Start() {
	go p.processLoop()
}

// processLoop continuously processes raw data from the queue
func (p *Processor) processLoop() {
	for rawData := range p.rawQueue {
		p.processRawData(rawData)
	}
}

// processRawData processes a single raw data item
func (p *Processor) processRawData(rawData *model.ScrapeRawData) {
	log.Printf("Processing raw data from target: %s", rawData.TargetURL)

	// Extract whitelist from filter config if present
	var whitelist []string
	if rawData.FilterConfig != nil {
		if enabled, ok := rawData.FilterConfig["enabled"].(bool); ok && enabled {
			if whitelistObj, ok := rawData.FilterConfig["whitelist"].([]interface{}); ok {
				whitelist = make([]string, 0, len(whitelistObj))
				for _, item := range whitelistObj {
					if str, ok := item.(string); ok {
						whitelist = append(whitelist, str)
					}
				}
			}
		}
	}

	// Convert the raw data to OpenMx format
	conversionResult, err := converter.Convert(rawData.RawData, whitelist)
	if err != nil {
		log.Printf("Error converting raw data: %v", err)
		return
	}

	// Add instance label to each OpenMx
	for _, openMx := range conversionResult.GetOpenMxList() {
		openMx.AddLabel("instance", rawData.TargetURL)
	}

	// Add instance property to each OpenMxHelp
	for _, openMxHelp := range conversionResult.GetOpenMxHelpList() {
		openMxHelp.Put("instance", rawData.TargetURL)
	}

	// Check if debug is enabled in whatap.conf
	if whatapConfig.IsDebugEnabled() {
		// Output metric data to stdout
		fmt.Println("=== DEBUG: Metrics Data ===")

		// Print OpenMx list
		fmt.Printf("OpenMx List (%d items):\n", len(conversionResult.GetOpenMxList()))
		for i, openMx := range conversionResult.GetOpenMxList() {
			// Convert to JSON for better readability
			jsonData, err := json.MarshalIndent(openMx, "", "  ")
			if err != nil {
				fmt.Printf("Error marshaling OpenMx to JSON: %v\n", err)
				continue
			}
			fmt.Printf("[%d] %s\n", i, string(jsonData))
		}

		// Print OpenMxHelp list
		fmt.Printf("\nOpenMxHelp List (%d items):\n", len(conversionResult.GetOpenMxHelpList()))
		for i, openMxHelp := range conversionResult.GetOpenMxHelpList() {
			// Convert to JSON for better readability
			jsonData, err := json.MarshalIndent(openMxHelp, "", "  ")
			if err != nil {
				fmt.Printf("Error marshaling OpenMxHelp to JSON: %v\n", err)
				continue
			}
			fmt.Printf("[%d] %s\n", i, string(jsonData))
		}

		fmt.Println("=== END DEBUG ===")
	}

	// Add the processed data to the queue
	p.processedQueue <- conversionResult
}
