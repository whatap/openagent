package processor

import (
	"math"
	"open-agent/tools/util/logutil"
	"strconv"
	"strings"
	"sync"

	"github.com/whatap/gointernal/net/secure"
	"open-agent/pkg/config"
	"open-agent/pkg/converter"
	"open-agent/pkg/model"
)

// Use the package-level functions provided by the config package
// instead of creating our own instance of WhatapConfig

// Processor is responsible for processing scraped metrics.
//
// Lifecycle: Start spawns an internal goroutine that drains rawQueue,
// converts and filters metrics, and pushes results to processedQueue.
// Stop signals that goroutine to exit and blocks until it has done so.
// Calling Start a second time without an intervening Stop is a no-op.
type Processor struct {
	rawQueue       chan *model.ScrapeRawData
	processedQueue chan *model.ConversionResult

	mu         sync.Mutex
	started    bool
	shutdownCh chan struct{}
	doneCh     chan struct{}
}

// NewProcessor creates a new Processor instance
func NewProcessor(rawQueue chan *model.ScrapeRawData, processedQueue chan *model.ConversionResult) *Processor {
	return &Processor{
		rawQueue:       rawQueue,
		processedQueue: processedQueue,
	}
}

// Start launches the processor goroutine. It is safe to call Start again
// after Stop, but redundant calls without Stop in between are ignored.
func (p *Processor) Start() {
	p.mu.Lock()
	if p.started {
		p.mu.Unlock()
		return
	}
	p.shutdownCh = make(chan struct{})
	p.doneCh = make(chan struct{})
	p.started = true
	p.mu.Unlock()

	go p.processLoop()
}

// Stop signals the processor goroutine to exit and waits for it. Stop is
// idempotent (calling it on a never-Started or already-Stopped Processor
// is a no-op).
func (p *Processor) Stop() {
	p.mu.Lock()
	if !p.started {
		p.mu.Unlock()
		return
	}
	p.started = false
	shutdownCh := p.shutdownCh
	doneCh := p.doneCh
	p.mu.Unlock()

	close(shutdownCh)
	<-doneCh
}

func (p *Processor) processLoop() {
	defer func() {
		if r := recover(); r != nil {
			logutil.Errorf("PROCESSOR", "Recovered from panic in processLoop: %v", r)
		}
		close(p.doneCh)
	}()

	for {
		select {
		case <-p.shutdownCh:
			logutil.Infoln("PROCESSOR", "Shutdown requested, exiting process loop")
			return
		case rawData, ok := <-p.rawQueue:
			if !ok {
				logutil.Infoln("PROCESSOR", "Raw queue closed, exiting process loop")
				return
			}
			p.processRawData(rawData)
		}
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

	// Set target and timestamp info
	conversionResult.SetTarget(rawData.TargetURL)
	conversionResult.SetCollectionTime(rawData.CollectionTime)

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
	pcode := secure.GetSecurityMaster().PCODE
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

	// Add the processed data to the queue.
	// Use a select so a pending shutdown can short-circuit a full queue
	// instead of pinning the processor goroutine on the channel send.
	select {
	case p.processedQueue <- conversionResult:
	case <-p.shutdownCh:
		logutil.Infoln("PROCESSOR", "Shutdown during enqueue, dropping last result")
	}
}
