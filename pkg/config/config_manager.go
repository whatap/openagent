package config

import (
	"fmt"
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	corev1 "k8s.io/api/core/v1"
	"open-agent/pkg/k8s"
)

// ConfigManager is responsible for loading and parsing the scrape configuration
type ConfigManager struct {
	config              map[string]interface{}
	configFile          string
	mu                  sync.RWMutex
	k8sClient           *k8s.K8sClient
	configMapNamespace  string
	configMapName       string
	onConfigReload      []func()
}

// NewConfigManager creates a new ConfigManager instance
func NewConfigManager() *ConfigManager {
	cm := &ConfigManager{
		configMapNamespace: "whatap-monitoring",
		configMapName:      "whatap-open-agent-config",
		onConfigReload:     make([]func(), 0),
	}

	// Initialize k8s client
	cm.k8sClient = k8s.GetInstance()

	// Register ConfigMap change handler if k8s client is initialized
	if cm.k8sClient.IsInitialized() {
		cm.k8sClient.RegisterConfigMapHandler(func(configMap *corev1.ConfigMap) {
			// Only handle our specific ConfigMap
			if configMap.Namespace == cm.configMapNamespace && configMap.Name == cm.configMapName {
				log.Printf("ConfigMap %s/%s changed, reloading configuration", configMap.Namespace, configMap.Name)

				// Get the scrape_config.yaml data from the ConfigMap
				if configData, ok := configMap.Data["scrape_config.yaml"]; ok {
					// Parse the YAML data
					var config map[string]interface{}
					err := yaml.Unmarshal([]byte(configData), &config)
					if err != nil {
						log.Printf("Error parsing ConfigMap data: %v", err)
						return
					}

					// Update the config with a lock to ensure thread safety
					cm.mu.Lock()
					cm.config = config
					cm.mu.Unlock()

					// Notify all registered handlers
					for _, handler := range cm.onConfigReload {
						handler()
					}
				}
			}
		})
	}

	// Load initial configuration
	err := cm.LoadConfig()
	if err != nil {
		log.Printf("Failed to load configuration: %v", err)
		return nil
	}

	return cm
}

// LoadConfig loads the configuration from the YAML file
func (cm *ConfigManager) LoadConfig() error {
	homeDir := os.Getenv("WHATAP_HOME")
	if homeDir == "" {
		homeDir = "."
	}
	configFile := filepath.Join(homeDir, "scrape_config.yaml")
	cm.configFile = configFile

	data, err := ioutil.ReadFile(configFile)
	if err != nil {
		return fmt.Errorf("error reading configuration file: %v", err)
	}

	var config map[string]interface{}
	err = yaml.Unmarshal(data, &config)
	if err != nil {
		return fmt.Errorf("error parsing configuration file: %v", err)
	}

	// Update the config with a lock to ensure thread safety
	cm.mu.Lock()
	cm.config = config
	cm.mu.Unlock()

	return nil
}

// GetConfig returns the entire configuration
func (cm *ConfigManager) GetConfig() map[string]interface{} {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
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

// GetScrapeConfigs returns the scrape_configs section or the openAgent.scrapConfigs/targets section if available
func (cm *ConfigManager) GetScrapeConfigs() []map[string]interface{} {
	// First, try to get the openAgent section from the CR format
	if cm.config != nil {
		// Check if we have a features section (CR format)
		if features, ok := cm.config["features"].(map[interface{}]interface{}); ok {
			// Check if we have an openAgent section
			if openAgent, ok := features["openAgent"].(map[interface{}]interface{}); ok {
				// Check if openAgent is enabled
				if enabled, ok := openAgent["enabled"].(bool); ok && enabled {
					// First check if we have a targets section (new format)
					if targets, ok := openAgent["targets"].([]interface{}); ok {
						result := make([]map[string]interface{}, 0, len(targets))
						for _, target := range targets {
							if targetMap, ok := target.(map[interface{}]interface{}); ok {
								// Convert map[interface{}]interface{} to map[string]interface{}
								stringMap := make(map[string]interface{})
								for k, v := range targetMap {
									if key, ok := k.(string); ok {
										stringMap[key] = convertToStringMap(v)
									}
								}
								// Add global settings if they exist
								if globalInterval, ok := openAgent["globalInterval"].(string); ok {
									if _, exists := stringMap["interval"]; !exists {
										stringMap["interval"] = globalInterval
									}
								}
								if globalPath, ok := openAgent["globalPath"].(string); ok {
									if _, exists := stringMap["path"]; !exists {
										stringMap["path"] = globalPath
									}
								}

								result = append(result, stringMap)
							}
						}
						return result
					}
				}
			}
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
