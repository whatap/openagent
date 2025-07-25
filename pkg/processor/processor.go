package processor

import (
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
	logutil.Printf("INFO", "[PROCESSOR] Processing raw data from target: %s", rawData.TargetURL)

	// Log metric relabel configs (simplified)
	if config.IsDebugEnabled() {
		logutil.Printf("DEBUG", "[PROCESSOR] Processing target %s with %d relabel configs",
			rawData.TargetURL, len(rawData.MetricRelabelConfigs))

		if len(rawData.MetricRelabelConfigs) > 0 {
			// Log only first config for debugging
			firstConfig := rawData.MetricRelabelConfigs[0]
			logutil.Printf("DEBUG", "[PROCESSOR] First relabel config: Action=%s, Regex=%s",
				firstConfig.Action, firstConfig.Regex)
		}
	}

	// Debug logging for node label functionality
	if config.IsDebugEnabled() && rawData.AddNodeLabel {
		if rawData.NodeName != "" {
			logutil.Printf("DEBUG", "[PROCESSOR] Node label will be added: node=%s", rawData.NodeName)
		} else {
			logutil.Printf("DEBUG", "[PROCESSOR] AddNodeLabel enabled but NodeName is empty")
		}
	}

	// Convert the raw data to OpenMx format using the collection timestamp
	conversionResult, err := converter.ConvertWithTimestamp(rawData.RawData, rawData.CollectionTime)
	if err != nil {
		logutil.Printf("ERROR", "[PROCESSOR] Error converting raw data: %v", err)
		return
	}

	// Apply metric relabeling if configured
	if len(rawData.MetricRelabelConfigs) > 0 {
		logutil.Printf("INFO", "[PROCESSOR] Applying %d metric relabel configs", len(rawData.MetricRelabelConfigs))
		converter.ApplyRelabelConfigs(conversionResult.GetOpenMxList(), rawData.MetricRelabelConfigs)
	}

	// Filter out metrics with NaN and infinite values
	filteredOpenMxList := make([]*model.OpenMx, 0, len(conversionResult.GetOpenMxList()))
	nodeLabelsAdded := 0
	totalValidMetrics := 0

	for _, openMx := range conversionResult.GetOpenMxList() {
		if !math.IsNaN(openMx.Value) && !math.IsInf(openMx.Value, 0) {
			totalValidMetrics++

			// Add instance label to each valid OpenMx
			openMx.AddLabel("instance", rawData.TargetURL)

			// Add node label if available and enabled
			if rawData.NodeName != "" && rawData.AddNodeLabel {
				openMx.AddLabel("node", rawData.NodeName)
				nodeLabelsAdded++
			}

			filteredOpenMxList = append(filteredOpenMxList, openMx)
		}
	}

	// Summary logging for node label addition
	if config.IsDebugEnabled() && nodeLabelsAdded > 0 {
		logutil.Printf("DEBUG", "[PROCESSOR] Added node labels to %d metrics", nodeLabelsAdded)
	}
	// Replace the original list with the filtered list
	conversionResult.OpenMxList = filteredOpenMxList

	// Add instance property to each OpenMxHelp
	nodePropertiesAdded := 0

	for _, openMxHelp := range conversionResult.GetOpenMxHelpList() {
		openMxHelp.Put("instance", rawData.TargetURL)

		// Add node property if available and enabled
		if rawData.NodeName != "" && rawData.AddNodeLabel {
			openMxHelp.Put("node", rawData.NodeName)
			nodePropertiesAdded++
		}
	}

	// Summary logging for node property addition
	if config.IsDebugEnabled() && nodePropertiesAdded > 0 {
		logutil.Printf("DEBUG", "[PROCESSOR] Added node properties to %d help items", nodePropertiesAdded)
	}

	// Summary logging for processed data
	if config.IsDebugEnabled() {
		validMetrics := 0
		for _, openMx := range conversionResult.GetOpenMxList() {
			if !math.IsNaN(openMx.Value) && !math.IsInf(openMx.Value, 0) {
				validMetrics++
			}
		}
		logutil.Printf("DEBUG", "[PROCESSOR] Processing complete: %d valid metrics, %d help items",
			validMetrics, len(conversionResult.GetOpenMxHelpList()))
	}

	// Add the processed data to the queue
	p.processedQueue <- conversionResult
}
