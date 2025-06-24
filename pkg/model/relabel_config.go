package model

// RelabelConfig represents a relabeling configuration for metrics
type RelabelConfig struct {
	SourceLabels []string `yaml:"source_labels,omitempty"`
	Separator    string   `yaml:"separator,omitempty"`
	TargetLabel  string   `yaml:"target_label,omitempty"`
	Regex        string   `yaml:"regex,omitempty"`
	Modulus      uint64   `yaml:"modulus,omitempty"`
	Replacement  string   `yaml:"replacement,omitempty"`
	Action       string   `yaml:"action,omitempty"`
}

// NewRelabelConfig creates a new RelabelConfig instance
func NewRelabelConfig() *RelabelConfig {
	return &RelabelConfig{
		SourceLabels: make([]string, 0),
		Separator:    ";",
		Regex:        "(.+)",
		Replacement:  "$1",
		Action:       "replace",
	}
}

// RelabelConfigs is a slice of RelabelConfig
type RelabelConfigs []*RelabelConfig

// NewRelabelConfigs creates a new RelabelConfigs instance
func NewRelabelConfigs() RelabelConfigs {
	return make(RelabelConfigs, 0)
}

// ParseRelabelConfigs parses a list of relabel configs from a generic interface
func ParseRelabelConfigs(configs []interface{}) RelabelConfigs {
	if configs == nil {
		return nil
	}

	result := make(RelabelConfigs, 0, len(configs))
	for _, c := range configs {
		if configMap, ok := c.(map[string]interface{}); ok {
			config := NewRelabelConfig()

			// Parse source_labels
			if sourceLabels, ok := configMap["source_labels"].([]interface{}); ok {
				for _, label := range sourceLabels {
					if labelStr, ok := label.(string); ok {
						config.SourceLabels = append(config.SourceLabels, labelStr)
					}
				}
			}

			// Parse separator
			if separator, ok := configMap["separator"].(string); ok {
				config.Separator = separator
			}

			// Parse target_label
			if targetLabel, ok := configMap["target_label"].(string); ok {
				config.TargetLabel = targetLabel
			}

			// Parse regex
			if regex, ok := configMap["regex"].(string); ok {
				config.Regex = regex
			}

			// Parse modulus
			if modulus, ok := configMap["modulus"].(uint64); ok {
				config.Modulus = modulus
			}

			// Parse replacement
			if replacement, ok := configMap["replacement"].(string); ok {
				config.Replacement = replacement
			}

			// Parse action
			if action, ok := configMap["action"].(string); ok {
				config.Action = action
			}

			result = append(result, config)
		}
	}

	return result
}
