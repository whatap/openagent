package processor

import (
	"math"
	"open-agent/tools/util/logutil"
	"strings"

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

func (p *Processor) Start() {
	go p.processLoop()
}

func (p *Processor) processLoop() {
	for rawData := range p.rawQueue {
		p.processRawData(rawData)
	}
}

func (p *Processor) processRawData(rawData *model.ScrapeRawData) {
	if config.IsDebugEnabled() {
		// Log only a preview of the raw metrics to avoid flooding logs
		const maxLines = 20
		raw := rawData.RawData
		lines := strings.Split(raw, "\n")
		previewLines := lines
		truncated := false
		if len(lines) > maxLines {
			previewLines = append([]string{}, lines[:maxLines]...)
			truncated = true
		}
		preview := strings.Join(previewLines, "\n")
		if truncated {
			preview += "\n... (truncated)"
		}
		logutil.Debugf("PROCESSOR", "Raw metrics preview (first %d lines of %d, target=%s):\n%s", maxLines, len(lines), rawData.TargetURL, preview)
	}
	logutil.Infof("PROCESSOR", "Processing raw data from target: %s", rawData.TargetURL)

	// Log metric relabel configs (simplified)
	if config.IsDebugEnabled() {
		logutil.Debugf("PROCESSOR", "Processing target %s with %d relabel configs",
			rawData.TargetURL, len(rawData.MetricRelabelConfigs))

		if len(rawData.MetricRelabelConfigs) > 0 {
			// Log only first config for debugging
			firstConfig := rawData.MetricRelabelConfigs[0]
			logutil.Debugf("PROCESSOR", "First relabel config: Action=%s, Regex=%s",
				firstConfig.Action, firstConfig.Regex)
		}
	}

	// Debug logging for node label functionality
	if config.IsDebugEnabled() && rawData.AddNodeLabel {
		if rawData.NodeName != "" {
			logutil.Debugf("PROCESSOR", "Node label will be added: node=%s", rawData.NodeName)
		} else {
			logutil.Debugf("PROCESSOR", "AddNodeLabel enabled but NodeName is empty")
		}
	}

	// Convert the raw data to OpenMx format using the collection timestamp
	conversionResult, err := converter.ConvertWithTimestamp(rawData.RawData, rawData.CollectionTime)
	if err != nil {
		logutil.Errorf("PROCESSOR", "Error converting raw data: %v", err)
		return
	}

	// Apply metric relabeling if configured
	if len(rawData.MetricRelabelConfigs) > 0 {
		logutil.Infof("PROCESSOR", "Applying %d metric relabel configs", len(rawData.MetricRelabelConfigs))
		converter.ApplyRelabelConfigs(conversionResult.GetOpenMxList(), rawData.MetricRelabelConfigs)
	}

	// Filter out metrics with NaN and infinite values
	filteredOpenMxList := make([]*model.OpenMx, 0, len(conversionResult.GetOpenMxList()))
	nodeLabelsAdded := 0
	totalValidMetrics := 0

	for _, openMx := range conversionResult.GetOpenMxList() {
		if !math.IsNaN(openMx.Value) && !math.IsInf(openMx.Value, 0) {
			totalValidMetrics++

			// Add target labels (including job and instance)
			for k, v := range rawData.Labels {
				openMx.AddLabel(k, v)
			}

			// Add instance label if missing (fallback for backward compatibility)
			if _, exists := rawData.Labels["instance"]; !exists {
				openMx.AddLabel("instance", rawData.TargetURL)
			}

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
		logutil.Debugf("PROCESSOR", "Added node labels to %d metrics", nodeLabelsAdded)
	}
	// Replace the original list with the filtered list
	conversionResult.OpenMxList = filteredOpenMxList

	// Summary logging for processed data
	if config.IsDebugEnabled() {
		validMetrics := 0
		for _, openMx := range conversionResult.GetOpenMxList() {
			if !math.IsNaN(openMx.Value) && !math.IsInf(openMx.Value, 0) {
				validMetrics++
			}
		}
		logutil.Debugf("PROCESSOR", "Processing complete: %d valid metrics, %d help items",
			validMetrics, len(conversionResult.GetOpenMxHelpList()))
	}

	// Add the processed data to the queue
	p.processedQueue <- conversionResult
}
