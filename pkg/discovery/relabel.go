package discovery

import (
	"crypto/md5"
	"encoding/binary"
	"fmt"
	"open-agent/pkg/model"
	"open-agent/tools/util/logutil"
	"regexp"
	"strings"
)

// ProcessRelabelConfigs applies relabel configs to the given labels.
// Returns the resulting labels and a boolean indicating whether the target should be kept.
func ProcessRelabelConfigs(labels map[string]string, configs model.RelabelConfigs) (map[string]string, bool) {
	// Make a copy of labels to work on
	resultLabels := make(map[string]string)
	for k, v := range labels {
		resultLabels[k] = v
	}

	if len(configs) == 0 {
		// Clean up meta labels (starting with __) and return
		finalLabels := make(map[string]string)
		for k, v := range resultLabels {
			if !strings.HasPrefix(k, "__") {
				finalLabels[k] = v
			}
		}
		return finalLabels, true
	}

	for _, config := range configs {
		// Get values of source labels
		values := make([]string, 0, len(config.SourceLabels))
		for _, sourceLabel := range config.SourceLabels {
			if val, ok := resultLabels[sourceLabel]; ok {
				values = append(values, val)
			} else {
				values = append(values, "")
			}
		}
		separator := config.Separator
		if separator == "" {
			separator = ";"
		}
		val := strings.Join(values, separator)

		regexStr := config.Regex
		if regexStr == "" {
			regexStr = "(.*)"
		}
		regex, err := regexp.Compile(regexStr)
		if err != nil {
			logutil.Errorf("RELABEL", "Invalid regex %q: %v", regexStr, err)
			continue
		}

		switch config.Action {
		case "drop":
			if regex.MatchString(val) {
				return nil, false // Drop target
			}
		case "keep":
			if !regex.MatchString(val) {
				return nil, false // Drop target
			}
		case "replace", "", "hashmod": // Default to replace
			if config.Action == "hashmod" {
				md5Sum := md5.Sum([]byte(val))
				mod := binary.BigEndian.Uint64(md5Sum[8:]) % config.Modulus
				targetLabel := config.TargetLabel
				if targetLabel != "" {
					resultLabels[targetLabel] = fmt.Sprintf("%d", mod)
				}
				continue
			}

			// Replace action
			if regex.MatchString(val) {
				replacement := config.Replacement
				if replacement == "" {
					replacement = "$1" // Default replacement
				}
				newVal := regex.ReplaceAllString(val, replacement)
				targetLabel := config.TargetLabel
				if targetLabel != "" {
					resultLabels[targetLabel] = newVal
				}
			}
		case "labelmap":
			for k, v := range resultLabels {
				if regex.MatchString(k) {
					replacement := config.Replacement
					if replacement == "" {
						replacement = "$1"
					}
					newKey := regex.ReplaceAllString(k, replacement)
					resultLabels[newKey] = v
				}
			}
		case "labeldrop":
			for k := range resultLabels {
				if regex.MatchString(k) {
					delete(resultLabels, k)
				}
			}
		case "labelkeep":
			for k := range resultLabels {
				if !regex.MatchString(k) {
					delete(resultLabels, k)
				}
			}
		}
	}

	// Clean up meta labels (starting with __)
	finalLabels := make(map[string]string)
	for k, v := range resultLabels {
		if !strings.HasPrefix(k, "__") {
			finalLabels[k] = v
		}
	}

	return finalLabels, true
}
