package processor

import (
	"encoding/json"
	"math"
	"open-agent/tools/util/logutil"

	"open-agent/pkg/config"
	"open-agent/pkg/converter"
	"open-agent/pkg/model"
)

// Use the package-level functions provided by the config package
// instead of creating our own instance of WhatapConfig

// Processor is responsible for processing scraped metrics
type Processor struct {
	rawQueue       chan *model.ScrapeRawData
	processedQueue chan *model.ConversionResult
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
	logutil.Printf("INFO", "Processing raw data from target: %s", rawData.TargetURL)

	// Convert the raw data to OpenMx format
	conversionResult, err := converter.Convert(rawData.RawData)
	if err != nil {
		logutil.Printf("ERROR", "Error converting raw data: %v", err)
		return
	}

	// Apply metric relabeling if configured
	if len(rawData.MetricRelabelConfigs) > 0 {
		logutil.Printf("INFO", "Applying metric relabeling with %d configs", len(rawData.MetricRelabelConfigs))
		converter.ApplyRelabelConfigs(conversionResult.GetOpenMxList(), rawData.MetricRelabelConfigs)
	}

	// Filter out metrics with NaN and infinite values
	filteredOpenMxList := make([]*model.OpenMx, 0, len(conversionResult.GetOpenMxList()))
	for _, openMx := range conversionResult.GetOpenMxList() {
		if !math.IsNaN(openMx.Value) && !math.IsInf(openMx.Value, 0) {
			// Add instance label to each valid OpenMx
			openMx.AddLabel("instance", rawData.TargetURL)

			// Add node label if available and enabled
			if rawData.NodeName != "" && rawData.AddNodeLabel {
				openMx.AddLabel("node", rawData.NodeName)
			}

			filteredOpenMxList = append(filteredOpenMxList, openMx)
		}
	}
	// Replace the original list with the filtered list
	conversionResult.OpenMxList = filteredOpenMxList

	// Add instance property to each OpenMxHelp
	for _, openMxHelp := range conversionResult.GetOpenMxHelpList() {
		openMxHelp.Put("instance", rawData.TargetURL)

		// Add node property if available and enabled
		if rawData.NodeName != "" && rawData.AddNodeLabel {
			openMxHelp.Put("node", rawData.NodeName)
		}
	}

	// Check if debug is enabled in whatap.conf
	if config.IsDebugEnabled() {
		// Output metric data to stdout
		logutil.Println("DEBUG", "=== DEBUG: Metrics Data ===")

		// Print OpenMx list
		logutil.Printf("DEBUG", "OpenMx List (%d items):\n", len(conversionResult.GetOpenMxList()))
		for i, openMx := range conversionResult.GetOpenMxList() {
			// Skip metrics with NaN values
			if math.IsNaN(openMx.Value) {
				logutil.Printf("DEBUG", "[%d] Skipped (NaN value)\n", i)
				continue
			}
			// Skip metrics with infinite values
			if math.IsInf(openMx.Value, 0) {
				logutil.Printf("DEBUG", "[%d] Skipped (infinite value: %v)\n", i, openMx.Value)
				continue
			}

			// Convert to JSON for better readability
			jsonData, err := json.MarshalIndent(openMx, "", "  ")
			if err != nil {
				logutil.Printf("ERROR", "Error marshaling OpenMx to JSON: %v\n", err)
				continue
			}
			logutil.Printf("DEBUG", "[%d] %s\n", i, string(jsonData))
		}

		// Print OpenMxHelp list
		logutil.Printf("DEBUG", "\nOpenMxHelp List (%d items):\n", len(conversionResult.GetOpenMxHelpList()))
		for i, openMxHelp := range conversionResult.GetOpenMxHelpList() {
			// Convert to JSON for better readability
			jsonData, err := json.MarshalIndent(openMxHelp, "", "  ")
			if err != nil {
				logutil.Printf("ERROR", "Error marshaling OpenMxHelp to JSON: %v\n", err)
				continue
			}
			logutil.Printf("DEBUG", "[%d] %s\n", i, string(jsonData))
		}

		logutil.Println("DEBUG", "=== END DEBUG ===")
	}

	// Add the processed data to the queue
	p.processedQueue <- conversionResult
}
