package processor

import (
	"math"
	"open-agent/tools/util/logutil"
	"strconv"
	"strings"

	"github.com/whatap/gointernal/net/secure"
	"open-agent/pkg/config"
	"open-agent/pkg/converter"
	"open-agent/pkg/dcgm"
	"open-agent/pkg/ec2state"
	"open-agent/pkg/model"
)

// Use the package-level functions provided by the config package
// instead of creating our own instance of WhatapConfig

// ec2StateSettingPrefix selects EC2 settings that may also come from container
// ENV: the WhaTap Operator configures OpenAgent through
// spec.features.openAgent.envs and does not render whatap.conf.
const ec2StateSettingPrefix = "ec2_state_"

func loadEC2StateSettings() map[string]string {
	return config.GetConfigMapWithEnvPrefix(ec2StateSettingPrefix)
}

// dcgmWeightedGPUUtilKey switches off deriving DCGM_FI_DEV_WEIGHTED_GPU_UTIL
// from dcgm-exporter scrapes (whatap.conf or container ENV, default on).
const dcgmWeightedGPUUtilKey = "dcgm_weighted_gpu_util_enabled"

func loadDCGMWeightedGPUUtilEnabled() bool {
	return config.GetBoolWithDefault(dcgmWeightedGPUUtilKey, true)
}

// Processor is responsible for processing scraped metrics
type Processor struct {
	rawQueue          chan *model.ScrapeRawData
	processedQueue    chan *model.ConversionResult
	ec2StateSettings  func() map[string]string
	enqueueEC2State   ec2state.Enqueue
	ec2StateForwarder ec2state.Forwarder
	securityMaster    func() *secure.SecurityMaster
	infof             func(id, format string, v ...interface{})
	weightedGPUUtil   func() bool
	weightedLogged    bool
}

// NewProcessor creates a new Processor instance
func NewProcessor(rawQueue chan *model.ScrapeRawData, processedQueue chan *model.ConversionResult) *Processor {
	return &Processor{
		rawQueue:         rawQueue,
		processedQueue:   processedQueue,
		securityMaster:   secure.GetSecurityMaster,
		ec2StateSettings: loadEC2StateSettings,
		enqueueEC2State:  secure.SendOneway,
		infof:            logutil.Infof,
		weightedGPUUtil:  loadDCGMWeightedGPUUtilEnabled,
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

	// Forward the original exporter observation before metric relabeling. Target
	// labels are display metadata, not authoritative EC2 identity/state. A failed
	// optional forward must not interrupt the ordinary metric pipeline.
	security := p.securityMaster()
	accepted, forwardErr := p.ec2StateForwarder.Forward(p.ec2StateSettings(), rawData, security.OID, p.enqueueEC2State)
	if forwardErr != nil {
		logutil.Errorf("EC2_STATE_FORWARD", "%s: locally queued=%d; %v", ec2state.CommandName, accepted, forwardErr)
	} else if accepted > 0 {
		// INFO so operators can confirm forwarding; scrapes that queue nothing stay silent.
		p.infof("EC2_STATE_FORWARD", "%s: locally queued=%d from %s (not a Yard acknowledgement)", ec2state.CommandName, accepted, rawData.TargetURL)
	}

	// Convert the raw data to OpenMx format using the collection timestamp.
	// The decoder (protobuf vs. text) is selected from the response Content-Type;
	// non-protobuf payloads fall back to the existing text parser.
	conversionResult, err := converter.ConvertWithContentType([]byte(rawData.RawData), rawData.ContentType, rawData.CollectionTime)
	if err != nil {
		logutil.Errorf("PROCESSOR", "Error converting raw data: %v", err)
		return
	}

	// Set target and timestamp info
	conversionResult.SetTarget(rawData.TargetURL)
	conversionResult.SetCollectionTime(rawData.CollectionTime)

	// The official dcgm-exporter does not publish WhaTap's per-GPU weighted
	// utilization. Derive it before relabeling so the rows follow the same
	// pipeline as rows a WhaTap exporter build publishes; those take precedence.
	if p.weightedGPUUtil() {
		if added := dcgm.AppendWeightedGPUUtil(conversionResult); added > 0 {
			if !p.weightedLogged {
				p.weightedLogged = true
				p.infof("PROCESSOR", "Derived %d %s rows from %s; exporter-published rows take precedence (%s=false disables)",
					added, dcgm.WeightedGPUUtilMetric, rawData.TargetURL, dcgmWeightedGPUUtilKey)
			} else if config.IsDebugEnabled() {
				logutil.Debugf("PROCESSOR", "Derived %d %s rows from %s", added, dcgm.WeightedGPUUtilMetric, rawData.TargetURL)
			}
		}
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

	// Get PCODE from SecurityMaster
	pcode := security.PCODE
	var pcodeStr string
	if pcode > 0 {
		pcodeStr = strconv.FormatInt(pcode, 10)
	}

	for _, openMx := range conversionResult.GetOpenMxList() {
		if !math.IsNaN(openMx.Value) && !math.IsInf(openMx.Value, 0) {
			totalValidMetrics++

			// Add target labels (including job and instance)
			for k, v := range rawData.Labels {
				openMx.AddLabel(k, v)
			}

			// Add pcode label
			if pcodeStr != "" {
				openMx.AddLabel("pcode", pcodeStr)
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
