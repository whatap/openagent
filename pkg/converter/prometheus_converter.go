package converter

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"open-agent/pkg/model"
)

const (
	helpText = "# HELP"
	typeText = "# TYPE"
)

// Convert converts Prometheus metrics to OpenMx format
func Convert(prometheusData string, whitelist []string) (*model.ConversionResult, error) {
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
			om, err := parseRecordLine(line)
			if err != nil {
				continue
			}

			// Filter by whitelist if provided
			if len(whitelist) > 0 {
				found := false
				for _, w := range whitelist {
					if w == om.Metric {
						found = true
						break
					}
				}
				if !found {
					continue
				}
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

// parseRecordLine parses a single line of Prometheus metrics data
func parseRecordLine(line string) (*model.OpenMx, error) {
	timestamp := time.Now().UnixMilli()
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
		labelContent := line[braceIndex+1:endBrace]
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

		// Parse value
		rest := strings.TrimSpace(line[endBrace+1:])
		if rest == "+Inf" {
			value = math.Inf(1) // Positive infinity
		} else if rest == "-Inf" {
			value = math.Inf(-1) // Negative infinity
		} else {
			var err error
			value, err = strconv.ParseFloat(rest, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid metric value: %v", err)
			}
		}
	} else {
		// No labels
		spaceIndex := strings.Index(line, " ")
		if spaceIndex == -1 {
			return nil, fmt.Errorf("invalid metric format: missing value")
		}

		metricName = strings.TrimSpace(line[:spaceIndex])
		valStr := strings.TrimSpace(line[spaceIndex+1:])

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
	}

	om := model.NewOpenMx(metricName, timestamp, value)
	for _, label := range labels {
		om.AddLabel(label.Key, label.Value)
	}

	return om, nil
}
