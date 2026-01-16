package converter

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	configPkg "open-agent/pkg/config"
	"open-agent/pkg/model"
	"open-agent/tools/util/logutil"
)

const (
	helpText = "# HELP"
	typeText = "# TYPE"
)

// Convert converts Prometheus metrics to OpenMx format
func Convert(prometheusData string) (*model.ConversionResult, error) {
	return ConvertWithTimestamp(prometheusData, time.Now().UnixMilli())
}

// ConvertWithTimestamp converts Prometheus metrics to OpenMx format using the provided timestamp
func ConvertWithTimestamp(prometheusData string, collectionTime int64) (*model.ConversionResult, error) {
	openMxList := make([]*model.OpenMx, 0)
	helpMap := make(map[string]*model.OpenMxHelp)

	lines := strings.Split(prometheusData, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, helpText) {
			content := strings.TrimSpace(line[len(helpText):])
			firstSpace := strings.Index(content, " ")
			if firstSpace < 0 {
				continue
			}
			metricName := content[:firstSpace]
			helpText := strings.TrimSpace(content[firstSpace+1:])

			omh := model.NewOpenMxHelp(metricName)
			omh.Put("help", helpText)
			helpMap[metricName] = omh
		} else if strings.HasPrefix(line, typeText) {
			content := strings.TrimSpace(line[len(typeText):])
			firstSpace := strings.Index(content, " ")
			if firstSpace < 0 {
				continue
			}
			metricName := content[:firstSpace]
			typeText := strings.TrimSpace(content[firstSpace+1:])

			if omh, ok := helpMap[metricName]; ok {
				omh.Put("type", typeText)
			}
		} else {
			om, err := parseRecordLine(line, collectionTime)
			if err != nil {
				continue
			}

			openMxList = append(openMxList, om)
		}
	}

	// Convert helpMap to a slice
	openMxHelpList := make([]*model.OpenMxHelp, 0, len(helpMap))
	for _, omh := range helpMap {
		openMxHelpList = append(openMxHelpList, omh)
	}

	return model.NewConversionResult(openMxList, openMxHelpList), nil
}

// matchMultipleWildcards checks if a string matches a pattern with multiple wildcards
// For example, "a*b*c" would match "axbyc", "axxbyyc", etc.
func matchMultipleWildcards(s string, parts []string) bool {
	if len(parts) == 0 {
		return s == ""
	}

	// Check if the string starts with the first part
	if parts[0] != "" && !strings.HasPrefix(s, parts[0]) {
		return false
	}

	// Check if the string ends with the last part
	if parts[len(parts)-1] != "" && !strings.HasSuffix(s, parts[len(parts)-1]) {
		return false
	}

	// Remove the first and last parts from the string
	if parts[0] != "" {
		s = s[len(parts[0]):]
	}
	if parts[len(parts)-1] != "" {
		s = s[:len(s)-len(parts[len(parts)-1])]
	}

	// Check the middle parts
	for i := 1; i < len(parts)-1; i++ {
		if parts[i] == "" {
			continue
		}

		idx := strings.Index(s, parts[i])
		if idx == -1 {
			return false
		}

		// Move past this part
		s = s[idx+len(parts[i]):]
	}

	return true
}

// ApplyRelabelConfigs applies relabeling configurations to a list of OpenMx objects
func ApplyRelabelConfigs(metrics []*model.OpenMx, configs model.RelabelConfigs) {
	if len(configs) == 0 {
		return
	}

	// Log the number of metrics and configs
	if configPkg.IsDebugEnabled() {
		logutil.Debugf("CONVERTER", "[CONVERTER] Processing %d metrics with %d relabel configs", len(metrics), len(configs))

		// Log the first few configs for debugging
		for i, config := range configs {
			if i < 3 { // Log only first 3 configs to avoid flooding logs
				logutil.Debugf("CONVERTER", "[CONVERTER] Config[%d]: Action=%s, SourceLabels=%v, Regex=%s",
					i, config.Action, config.SourceLabels, config.Regex)
			}
		}
	}

	// Track metrics that are kept and dropped
	keptCount := 0
	droppedCount := 0

	for _, metric := range metrics {
		originalValue := metric.Value
		isNaN := math.IsNaN(originalValue)

		for _, config := range configs {
			applyRelabelConfig(metric, config)
		}

		// Check if metric was kept or dropped
		if !isNaN && math.IsNaN(metric.Value) {
			droppedCount++
			// Log some dropped metrics for debugging
			if configPkg.IsDebugEnabled() && droppedCount <= 5 {
				logutil.Debugf("CONVERTER", "[CONVERTER] Metric dropped: %s", metric.Metric)
			}
		} else if !math.IsNaN(metric.Value) {
			keptCount++
			// Log some kept metrics for debugging
			if configPkg.IsDebugEnabled() && keptCount <= 5 {
				logutil.Debugf("CONVERTER", "[CONVERTER] Metric kept: %s", metric.Metric)
			}
		}
	}

	if configPkg.IsDebugEnabled() {
		logutil.Debugf("CONVERTER", "[CONVERTER] Relabel result: %d metrics kept, %d metrics dropped",
			keptCount, droppedCount)
	}
}

// applyRelabelConfig applies a single relabeling configuration to an OpenMx object
func applyRelabelConfig(metric *model.OpenMx, config *model.RelabelConfig) {
	// Skip if no action is specified
	if config.Action == "" {
		return
	}

	// Handle different actions
	switch config.Action {
	case "keep":
		// Keep metrics that match the regex
		if !matchesRegex(metric, config) {
			// Mark for removal by setting Value to NaN
			metric.Value = math.NaN()
		}
	case "drop":
		// Drop metrics that match the regex
		if matchesRegex(metric, config) {
			// Mark for removal by setting Value to NaN
			metric.Value = math.NaN()
		}
	case "replace":
		// Replace the value of the target label with the regex replacement
		if config.TargetLabel != "" {
			replacement := getReplacementValue(metric, config)
			// Find and update existing label or add a new one
			found := false
			for i, label := range metric.Labels {
				if label.Key == config.TargetLabel {
					metric.Labels[i].Value = replacement
					found = true
					break
				}
			}
			if !found {
				metric.AddLabel(config.TargetLabel, replacement)
			}
		}
	case "labelmap":
		// Map labels that match the regex to new labels
		// Not implemented yet
	case "labelkeep":
		// Keep labels that match the regex
		// Not implemented yet
	case "labeldrop":
		// Drop labels that match the regex
		// Not implemented yet
	}
}

// matchesRegex checks if a metric matches the regex in the relabel config
func matchesRegex(metric *model.OpenMx, config *model.RelabelConfig) bool {
	// For detailed debugging of specific metrics (add metric names you want to debug)
	debugThisMetric := false
	metricsToDebug := []string{"apiserver_request_total", "apiserver_current_inflight_requests", "etcd_server_requests_total"}
	for _, m := range metricsToDebug {
		if strings.Contains(metric.Metric, m) {
			debugThisMetric = true
			break
		}
	}

	// If no source labels are specified, use the metric name
	if len(config.SourceLabels) == 0 {
		re, err := regexp.Compile(config.Regex)
		if err != nil {
			if configPkg.IsDebugEnabled() && debugThisMetric {
				logutil.Debugf("CONVERTER", "[CONVERTER] Error compiling regex '%s': %v", config.Regex, err)
			}
			return false
		}

		matches := re.MatchString(metric.Metric)
		if configPkg.IsDebugEnabled() && debugThisMetric {
			logutil.Debugf("CONVERTER", "[CONVERTER] Metric '%s' against regex '%s' => %v",
				metric.Metric, config.Regex, matches)
		}
		return matches
	}

	// Extract values from source labels
	var values []string
	for _, sourceLabel := range config.SourceLabels {
		if sourceLabel == "__name__" {
			values = append(values, metric.Metric)
			if configPkg.IsDebugEnabled() && debugThisMetric {
				logutil.Debugf("CONVERTER", "[CONVERTER] Using __name__ = '%s'", metric.Metric)
			}
		} else {
			// Find the label value
			found := false
			for _, label := range metric.Labels {
				if label.Key == sourceLabel {
					values = append(values, label.Value)
					found = true
					if configPkg.IsDebugEnabled() && debugThisMetric {
						logutil.Debugf("CONVERTER", "[CONVERTER] Found label %s = '%s'", sourceLabel, label.Value)
					}
					break
				}
			}
			if !found {
				values = append(values, "")
				if configPkg.IsDebugEnabled() && debugThisMetric {
					logutil.Debugf("CONVERTER", "[CONVERTER] Label %s not found, using empty string", sourceLabel)
				}
			}
		}
	}

	// Concatenate values with separator
	value := strings.Join(values, config.Separator)
	if configPkg.IsDebugEnabled() && debugThisMetric {
		logutil.Debugf("CONVERTER", "[CONVERTER] Concatenated value: '%s'", value)
	}

	// Match against regex
	re, err := regexp.Compile(config.Regex)
	if err != nil {
		if configPkg.IsDebugEnabled() && debugThisMetric {
			logutil.Debugf("CONVERTER", "[CONVERTER] Error compiling regex '%s': %v", config.Regex, err)
		}
		return false
	}

	matches := re.MatchString(value)
	if configPkg.IsDebugEnabled() && debugThisMetric {
		logutil.Debugf("CONVERTER", "[CONVERTER] Value '%s' against regex '%s' => %v",
			value, config.Regex, matches)
	}
	return matches
}

// getReplacementValue gets the replacement value for a label
func getReplacementValue(metric *model.OpenMx, config *model.RelabelConfig) string {
	// If no source labels are specified, use the replacement value directly
	if len(config.SourceLabels) == 0 {
		return config.Replacement
	}

	// Extract values from source labels
	var values []string
	for _, sourceLabel := range config.SourceLabels {
		if sourceLabel == "__name__" {
			values = append(values, metric.Metric)
		} else {
			// Find the label value
			found := false
			for _, label := range metric.Labels {
				if label.Key == sourceLabel {
					values = append(values, label.Value)
					found = true
					break
				}
			}
			if !found {
				values = append(values, "")
			}
		}
	}

	// Concatenate values with separator
	value := strings.Join(values, config.Separator)

	// Apply regex replacement
	re, err := regexp.Compile(config.Regex)
	if err != nil {
		return config.Replacement
	}
	return re.ReplaceAllString(value, config.Replacement)
}

// parseRecordLine parses a single line of Prometheus metrics data
func parseRecordLine(line string, timestamp int64) (*model.OpenMx, error) {
	var metricName string
	var value float64
	var labels []model.Label

	// Check if the line has labels
	braceIndex := strings.Index(line, "{")
	if braceIndex != -1 {
		metricName = strings.TrimSpace(line[:braceIndex])
		endBrace := strings.Index(line, "}")
		if endBrace == -1 {
			return nil, fmt.Errorf("invalid metric format: missing closing brace")
		}

		// Parse labels
		labelContent := line[braceIndex+1 : endBrace]
		if labelContent != "" {
			labelPairs := strings.Split(labelContent, ",")
			for _, pair := range labelPairs {
				kv := strings.SplitN(pair, "=", 2)
				if len(kv) == 2 {
					key := strings.TrimSpace(kv[0])
					val := strings.TrimSpace(kv[1])

					// Remove quotes if present
					if strings.HasPrefix(val, "\"") && strings.HasSuffix(val, "\"") && len(val) >= 2 {
						val = val[1 : len(val)-1]
					}

					labels = append(labels, model.Label{Key: key, Value: val})
				}
			}
		}

		// Parse value and optional timestamp
		// Format: "value" or "value timestamp"
		rest := strings.TrimSpace(line[endBrace+1:])
		parts := strings.Fields(rest)
		if len(parts) == 0 {
			return nil, fmt.Errorf("invalid metric format: missing value")
		}

		valStr := parts[0]
		if valStr == "+Inf" {
			value = math.Inf(1) // Positive infinity
		} else if valStr == "-Inf" {
			value = math.Inf(-1) // Negative infinity
		} else {
			var err error
			value, err = strconv.ParseFloat(valStr, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid metric value: %v", err)
			}
		}

		// If timestamp is present in the metric line, use it instead of the provided timestamp
		if len(parts) >= 2 {
			if ts, err := strconv.ParseInt(parts[1], 10, 64); err == nil {
				timestamp = ts
			}
		}
	} else {
		// No labels
		// Format: "metric_name value" or "metric_name value timestamp"
		parts := strings.Fields(line)
		if len(parts) < 2 {
			return nil, fmt.Errorf("invalid metric format: missing value")
		}

		metricName = parts[0]
		valStr := parts[1]

		if valStr == "+Inf" {
			value = math.Inf(1) // Positive infinity
		} else if valStr == "-Inf" {
			value = math.Inf(-1) // Negative infinity
		} else {
			var err error
			value, err = strconv.ParseFloat(valStr, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid metric value: %v", err)
			}
		}

		// If timestamp is present in the metric line, use it instead of the provided timestamp
		if len(parts) >= 3 {
			if ts, err := strconv.ParseInt(parts[2], 10, 64); err == nil {
				timestamp = ts
			}
		}
	}

	om := model.NewOpenMx(metricName, timestamp, value)
	for _, label := range labels {
		om.AddLabel(label.Key, label.Value)
	}

	return om, nil
}
