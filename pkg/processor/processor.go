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

	// ===== METRIC RELABEL CONFIGS LOGGING (Before Processing) =====
	logutil.Printf("METRIC_RELABEL", "=== MetricRelabelConfigs Status ===")
	logutil.Printf("METRIC_RELABEL", "Target: %s", rawData.TargetURL)
	logutil.Printf("METRIC_RELABEL", "Total MetricRelabelConfigs: %d", len(rawData.MetricRelabelConfigs))

	if len(rawData.MetricRelabelConfigs) == 0 {
		logutil.Printf("METRIC_RELABEL", "No MetricRelabelConfigs found - all metrics will be kept")
	} else {
		logutil.Printf("METRIC_RELABEL", "MetricRelabelConfigs details:")
		for i, config := range rawData.MetricRelabelConfigs {
			logutil.Printf("METRIC_RELABEL", "  Config[%d]:", i)
			logutil.Printf("METRIC_RELABEL", "    Action: %s", config.Action)
			logutil.Printf("METRIC_RELABEL", "    SourceLabels: %v", config.SourceLabels)
			logutil.Printf("METRIC_RELABEL", "    Regex: %s", config.Regex)
			logutil.Printf("METRIC_RELABEL", "    TargetLabel: %s", config.TargetLabel)
			logutil.Printf("METRIC_RELABEL", "    Replacement: %s", config.Replacement)
			logutil.Printf("METRIC_RELABEL", "    Separator: %s", config.Separator)
		}
	}
	logutil.Printf("METRIC_RELABEL", "=== End MetricRelabelConfigs Status ===")

	// Debug logging for addNodeLabel functionality
	logutil.Printf("DEBUG_NODE", "NodeName: '%s', AddNodeLabel: %v", rawData.NodeName, rawData.AddNodeLabel)
	if rawData.AddNodeLabel && rawData.NodeName != "" {
		logutil.Printf("DEBUG_NODE", "Node label will be added to metrics: node=%s", rawData.NodeName)
	} else if rawData.AddNodeLabel && rawData.NodeName == "" {
		logutil.Printf("DEBUG_NODE", "AddNodeLabel is true but NodeName is empty - no node label will be added")
	} else if !rawData.AddNodeLabel && rawData.NodeName != "" {
		logutil.Printf("DEBUG_NODE", "NodeName is available (%s) but AddNodeLabel is false - no node label will be added", rawData.NodeName)
	} else {
		logutil.Printf("DEBUG_NODE", "No node label will be added (AddNodeLabel: %v, NodeName: '%s')", rawData.AddNodeLabel, rawData.NodeName)
	}

	// Convert the raw data to OpenMx format using the collection timestamp
	conversionResult, err := converter.ConvertWithTimestamp(rawData.RawData, rawData.CollectionTime)
	if err != nil {
		logutil.Printf("ERROR", "Error converting raw data: %v", err)
		return
	}

	// Apply metric relabeling if configured
	if len(rawData.MetricRelabelConfigs) > 0 {
		logutil.Printf("INFO", "Applying metric relabeling with %d configs", len(rawData.MetricRelabelConfigs))

		// Detailed logging of metric relabel configs
		for i, metricRelabelConfig := range rawData.MetricRelabelConfigs {
			logutil.Printf("DEBUG", "MetricRelabelConfig[%d]: Action=%s, SourceLabels=%v, Regex=%s", 
				i, metricRelabelConfig.Action, metricRelabelConfig.SourceLabels, metricRelabelConfig.Regex)
		}

		// Log metrics before applying relabel configs
		if config.IsDebugEnabled() {
			logutil.Printf("DEBUG", "Before applying relabel configs: %d metrics", len(conversionResult.GetOpenMxList()))
			for i, metric := range conversionResult.GetOpenMxList() {
				if i < 5 { // Log only first 5 metrics to avoid flooding logs
					logutil.Printf("DEBUG", "Metric[%d] before relabeling: %s, Value=%v", i, metric.Metric, metric.Value)
				}
			}
		}

		converter.ApplyRelabelConfigs(conversionResult.GetOpenMxList(), rawData.MetricRelabelConfigs)

		// Log metrics after applying relabel configs
		if config.IsDebugEnabled() {
			// Count non-NaN metrics
			nonNanCount := 0
			for _, metric := range conversionResult.GetOpenMxList() {
				if !math.IsNaN(metric.Value) {
					nonNanCount++
				}
			}
			if config.IsDebugEnabled() {
				logutil.Printf("DEBUG", "After applying relabel configs: %d non-NaN metrics out of %d total", 
					nonNanCount, len(conversionResult.GetOpenMxList()))
			}
		}
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

				// Log first few metrics that get node labels for debugging
				if config.IsDebugEnabled() && nodeLabelsAdded <= 3 {
					logutil.Printf("DEBUG_NODE", "Added node label to metric[%d]: %s, node=%s", 
						nodeLabelsAdded, openMx.Metric, rawData.NodeName)
				}
			}

			filteredOpenMxList = append(filteredOpenMxList, openMx)
		}
	}

	// Summary logging for node label addition
	if config.IsDebugEnabled() && rawData.AddNodeLabel && rawData.NodeName != "" {
		logutil.Printf("DEBUG_NODE", "Node label addition summary: %d out of %d valid metrics received node label (node=%s)", 
			nodeLabelsAdded, totalValidMetrics, rawData.NodeName)
	}
	// Replace the original list with the filtered list
	conversionResult.OpenMxList = filteredOpenMxList

	// Add instance property to each OpenMxHelp
	nodePropertiesAdded := 0
	totalHelpItems := len(conversionResult.GetOpenMxHelpList())

	for i, openMxHelp := range conversionResult.GetOpenMxHelpList() {
		openMxHelp.Put("instance", rawData.TargetURL)

		// Add node property if available and enabled
		if rawData.NodeName != "" && rawData.AddNodeLabel {
			openMxHelp.Put("node", rawData.NodeName)
			nodePropertiesAdded++

			// Log first few help items that get node properties for debugging
			if config.IsDebugEnabled() && nodePropertiesAdded <= 3 {
				logutil.Printf("DEBUG_NODE", "Added node property to OpenMxHelp[%d]: metric=%s, node=%s", 
					i+1, openMxHelp.Metric, rawData.NodeName)
			}
		}
	}

	// Summary logging for node property addition
	if config.IsDebugEnabled() && rawData.AddNodeLabel && rawData.NodeName != "" {
		logutil.Printf("DEBUG_NODE", "Node property addition summary: %d out of %d help items received node property (node=%s)", 
			nodePropertiesAdded, totalHelpItems, rawData.NodeName)
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
