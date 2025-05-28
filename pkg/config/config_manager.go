package config

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"gopkg.in/yaml.v2"
)

// ConfigManager is responsible for loading and parsing the scrape configuration
type ConfigManager struct {
	config map[string]interface{}
}

// NewConfigManager creates a new ConfigManager instance
func NewConfigManager() *ConfigManager {
	cm := &ConfigManager{}
	cm.LoadConfig()
	return cm
}

// LoadConfig loads the configuration from the YAML file
func (cm *ConfigManager) LoadConfig() error {
	homeDir := os.Getenv("WHATAP_HOME")
	if homeDir == "" {
		homeDir = "."
	}
	configFile := filepath.Join(homeDir, "scrape_config.yaml")

	data, err := ioutil.ReadFile(configFile)
	if err != nil {
		return fmt.Errorf("error reading configuration file: %v", err)
	}

	var config map[string]interface{}
	err = yaml.Unmarshal(data, &config)
	if err != nil {
		return fmt.Errorf("error parsing configuration file: %v", err)
	}

	cm.config = config
	return nil
}

// GetConfig returns the entire configuration
func (cm *ConfigManager) GetConfig() map[string]interface{} {
	return cm.config
}

// GetScrapeInterval returns the global scrape interval
func (cm *ConfigManager) GetScrapeInterval() string {
	if cm.config != nil {
		if global, ok := cm.config["global"].(map[interface{}]interface{}); ok {
			if interval, ok := global["scrape_interval"].(string); ok {
				return interval
			}
		}
	}
	return "15s"
}

// GetScrapeConfigs returns the scrape_configs section
func (cm *ConfigManager) GetScrapeConfigs() []map[string]interface{} {
	if cm.config != nil {
		if configs, ok := cm.config["scrape_configs"].([]interface{}); ok {
			result := make([]map[string]interface{}, 0, len(configs))
			for _, config := range configs {
				if configMap, ok := config.(map[interface{}]interface{}); ok {
					// Convert map[interface{}]interface{} to map[string]interface{}
					stringMap := make(map[string]interface{})
					for k, v := range configMap {
						if key, ok := k.(string); ok {
							stringMap[key] = convertToStringMap(v)
						}
					}
					result = append(result, stringMap)
				}
			}
			return result
		}
	}
	return nil
}

// ParseInterval parses an interval string (e.g., "15s", "1m") to seconds
func (cm *ConfigManager) ParseInterval(intervalStr string) (int64, error) {
	if intervalStr == "" {
		return 15, nil // Default to 15 seconds
	}

	if strings.HasSuffix(intervalStr, "s") {
		seconds, err := strconv.ParseInt(intervalStr[:len(intervalStr)-1], 10, 64)
		if err != nil {
			return 0, err
		}
		return seconds, nil
	} else if strings.HasSuffix(intervalStr, "m") {
		minutes, err := strconv.ParseInt(intervalStr[:len(intervalStr)-1], 10, 64)
		if err != nil {
			return 0, err
		}
		return minutes * 60, nil
	} else {
		seconds, err := strconv.ParseInt(intervalStr, 10, 64)
		if err != nil {
			return 0, err
		}
		return seconds, nil
	}
}

// Helper function to convert interface{} values to string maps recursively
func convertToStringMap(value interface{}) interface{} {
	switch v := value.(type) {
	case map[interface{}]interface{}:
		result := make(map[string]interface{})
		for k, val := range v {
			if key, ok := k.(string); ok {
				result[key] = convertToStringMap(val)
			}
		}
		return result
	case []interface{}:
		result := make([]interface{}, len(v))
		for i, val := range v {
			result[i] = convertToStringMap(val)
		}
		return result
	default:
		return v
	}
}