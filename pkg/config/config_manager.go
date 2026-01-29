package config

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v2"

	"open-agent/pkg/k8s"
	"open-agent/tools/util/logutil"
)

// Force standalone mode flag
var forceStandaloneMode bool = false

// SetForceStandaloneMode sets the force standalone mode flag
func SetForceStandaloneMode(force bool) {
	forceStandaloneMode = force
}

// IsForceStandaloneMode returns the current force standalone mode flag
func IsForceStandaloneMode() bool {
	return forceStandaloneMode
}

// ConfigManager is responsible for loading and parsing the scrape configuration
type ConfigManager struct {
	config             map[string]interface{}
	configFile         string
	mu                 sync.RWMutex
	k8sClient          *k8s.K8sClient
	configMapNamespace string
	configMapName      string
	fileWatcherEnabled bool
	fileWatcherStop    chan struct{}
	lastModTime        time.Time
}

// getPodNamespace returns the namespace of the current pod from the ServiceAccount mount
func getPodNamespace() string {
	// Try environment variable first (Downward API)
	if namespace := os.Getenv("POD_NAMESPACE"); namespace != "" {
		return namespace
	}

	// Try reading from ServiceAccount mount (available in Pod)
	data, err := ioutil.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// NewConfigManager creates a new ConfigManager instance
func NewConfigManager() *ConfigManager {
	// Auto-detect namespace from Pod environment
	namespace := getPodNamespace()
	if namespace == "" {
		// Fallback to default namespace
		namespace = "whatap-monitoring"
		logutil.Infof("CONFIG", "Could not detect pod namespace, using default: %s", namespace)
	} else {
		logutil.Infof("CONFIG", "Detected pod namespace: %s", namespace)
	}

	cm := &ConfigManager{
		configMapNamespace: namespace,
		configMapName:      "whatap-open-agent-config",
	}

	// Check force standalone mode first
	if forceStandaloneMode {
		logutil.Infof("CONFIG", "Force standalone configuration mode enabled, using file watcher")
		// Set k8s client to standalone mode to prevent initialization
		k8s.SetStandaloneMode(true)

		cm.fileWatcherEnabled = true
		cm.fileWatcherStop = make(chan struct{})

		go cm.watchConfigFile()
		// Initial file load
		if err := cm.LoadConfig(); err != nil {
			logutil.Infof("CONFIG", "Failed to load configuration: %v", err)
			return nil
		}
		return cm
	}
	// Initialize k8s client
	cm.k8sClient = k8s.GetInstance()

	if cm.k8sClient.IsInitialized() {
		logutil.Infof("CONFIG", "Kubernetes environment detected, using ConfigMap informer cache")

		// Initial configuration load
		if err := cm.LoadConfig(); err != nil {
			logutil.Infof("CONFIG", "Failed to load initial configuration: %v", err)
			return nil
		}

	} else {
		// Non-k8s environment: file watcher
		logutil.Infof("CONFIG", "Non-Kubernetes environment detected, using file watcher")
		cm.fileWatcherEnabled = true
		cm.fileWatcherStop = make(chan struct{})
		go cm.watchConfigFile()

		// Initial file load
		if err := cm.LoadConfig(); err != nil {
			logutil.Infof("CONFIG", "Failed to load configuration: %v", err)
			return nil
		}
	}

	return cm
}

// LoadConfig loads the configuration from informer cache or YAML file
func (cm *ConfigManager) LoadConfig() error {
	// k8s environment: use informer cache directly
	if cm.k8sClient != nil && cm.k8sClient.IsInitialized() && forceStandaloneMode == false {
		configMap, err := cm.k8sClient.GetConfigMap(cm.configMapNamespace, cm.configMapName)
		if err != nil || configMap == nil {
			return fmt.Errorf("ConfigMap %s/%s not found: %v", cm.configMapNamespace, cm.configMapName, err)
		}

		configData, ok := configMap.Data["scrape_config.yaml"]
		if !ok {
			return fmt.Errorf("scrape_config.yaml not found in ConfigMap")
		}

		var config map[string]interface{}
		err = yaml.Unmarshal([]byte(configData), &config)
		if err != nil {
			return fmt.Errorf("error parsing ConfigMap data: %v", err)
		}

		cm.mu.Lock()
		cm.config = config
		cm.mu.Unlock()
		if IsDebugEnabled() {
			logutil.Debugf("CONFIG", "Configuration loaded from ConfigMap informer cache")
		}
		return nil
	}

	// Fall back to local file
	homeDir := os.Getenv("WHATAP_OPEN_HOME")
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

	logutil.Infof("CONFIG", "Configuration loaded from local file %s", configFile)
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

func (cm *ConfigManager) GetScrapeConfigs() []map[string]interface{} {
	// Always reload configuration from Informer cache in Kubernetes environment
	if IsDebugEnabled() {
		logutil.Debugf("GetScrapeConfigs", "START")
	}
	if cm.k8sClient != nil && cm.k8sClient.IsInitialized() {
		if IsDebugEnabled() {
			logutil.Debugf("GetScrapeConfigs", "Kubernetes environment detected, using ConfigMap informer cache")
			logutil.Debugf("CONFIG", "GetScrapeConfigs: Reloading latest configuration from Informer cache")
		}
		if err := cm.LoadConfig(); err != nil {
			if IsDebugEnabled() {
				logutil.Debugf("CONFIG", "GetScrapeConfigs: Failed to reload config from Informer cache: %v", err)
			}
			// Continue with existing config as fallback
		} else {
			if IsDebugEnabled() {
				logutil.Debugf("CONFIG", "GetScrapeConfigs: Successfully reloaded configuration from Informer cache")
			}
		}
	}
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if IsDebugEnabled() {
		logutil.Debugf("CONFIG", "GetScrapeConfigs: Processing current configuration")
	}

	// First, try to get the openAgent section from the CR format
	if cm.config != nil {
		// Check if we have a features section (CR format)
		if features, ok := cm.config["features"].(map[interface{}]interface{}); ok {
			// Check if we have an openAgent section
			if openAgent, ok := features["openAgent"].(map[interface{}]interface{}); ok {
				// Check if openAgent is enabled
				if enabled, ok := openAgent["enabled"].(bool); ok && enabled {
					if IsDebugEnabled() {
						logutil.Debugf("CONFIG", "GetScrapeConfigs: OpenAgent is enabled, checking for targets")
					}
					// First check if we have a targets section (new format)
					if targets, ok := openAgent["targets"].([]interface{}); ok {
						if IsDebugEnabled() {
							logutil.Debugf("CONFIG", "GetScrapeConfigs: Found %d targets in configuration", len(targets))
						}
						result := make([]map[string]interface{}, 0, len(targets))
						for i, target := range targets {
							if targetMap, ok := target.(map[interface{}]interface{}); ok {
								// Convert map[interface{}]interface{} to map[string]interface{}
								stringMap := make(map[string]interface{})
								for k, v := range targetMap {
									if key, ok := k.(string); ok {
										stringMap[key] = convertToStringMap(v)
									}
								}

								// Log target name for debugging
								if targetName, ok := stringMap["targetName"].(string); ok {
									if IsDebugEnabled() {
										logutil.Debugf("CONFIG", "GetScrapeConfigs: Processing target %d: %s", i+1, targetName)
									}
								}

								result = append(result, stringMap)
							}
						}
						if IsDebugEnabled() {
							logutil.Debugf("CONFIG", "GetScrapeConfigs: Returning %d processed targets", len(result))
						}
						return result
					} else {
						if IsDebugEnabled() {
							logutil.Debugf("CONFIG", "GetScrapeConfigs: No targets section found in openAgent configuration")
						}
					}
				} else {
					if IsDebugEnabled() {
						logutil.Debugf("CONFIG", "GetScrapeConfigs: OpenAgent is disabled or enabled flag not found")
					}
				}
			} else {
				if IsDebugEnabled() {
					logutil.Debugf("CONFIG", "GetScrapeConfigs: No openAgent section found in features")
				}
			}
		} else {
			if IsDebugEnabled() {
				logutil.Debugf("CONFIG", "GetScrapeConfigs: No features section found in configuration")
			}
		}
	} else {
		if IsDebugEnabled() {
			logutil.Debugf("CONFIG", "GetScrapeConfigs: Configuration is nil")
		}
	}

	if IsDebugEnabled() {
		logutil.Debugf("CONFIG", "GetScrapeConfigs: No valid scrape configs found, returning nil")
	}
	return nil
}

// watchConfigFile periodically checks if the configuration file has been modified
// and reloads it if necessary
func (cm *ConfigManager) watchConfigFile() {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	// Get initial modification time
	if fileInfo, err := os.Stat(cm.configFile); err == nil {
		cm.lastModTime = fileInfo.ModTime()
	}

	for {
		select {
		case <-ticker.C:
			if fileInfo, err := os.Stat(cm.configFile); err == nil {
				if fileInfo.ModTime().After(cm.lastModTime) {
					logutil.Infof("CONFIG", "Configuration file changed, automatically synchronizing")
					cm.lastModTime = fileInfo.ModTime()

					if err := cm.LoadConfig(); err != nil {
						logutil.Infof("CONFIG", "Error reloading configuration: %v", err)
						continue
					}

					// Handler execution removed - simple configuration update only
					logutil.Infof("CONFIG", "Configuration synchronized successfully")
				}
			}
		case <-cm.fileWatcherStop:
			return
		}
	}
}

// Close stops the file watcher if it's running
func (cm *ConfigManager) Close() {
	if cm.fileWatcherEnabled {
		close(cm.fileWatcherStop)
	}
}

// GetScrapingInterval returns the scraping loop interval from openAgent configuration
func (cm *ConfigManager) GetScrapingInterval() string {
	if cm.config != nil {
		if features, ok := cm.config["features"].(map[interface{}]interface{}); ok {
			if openAgent, ok := features["openAgent"].(map[interface{}]interface{}); ok {
				if scrapingInterval, ok := openAgent["scrapingInterval"].(string); ok {
					return scrapingInterval
				}
			}
		}
	}
	return "30s"
}

// GetMaxConcurrency returns the maximum concurrent scrapers from openAgent configuration
func (cm *ConfigManager) GetMaxConcurrency() int {
	if cm.config != nil {
		if features, ok := cm.config["features"].(map[interface{}]interface{}); ok {
			if openAgent, ok := features["openAgent"].(map[interface{}]interface{}); ok {
				if maxConcurrency, ok := openAgent["maxConcurrency"].(int); ok {
					return maxConcurrency
				}
			}
		}
	}
	return 0 // 0 means dynamic based on target count
}

// GetMinimumInterval returns the minimum scraping interval from openAgent configuration
func (cm *ConfigManager) GetMinimumInterval() string {
	if cm.config != nil {
		if features, ok := cm.config["features"].(map[interface{}]interface{}); ok {
			if openAgent, ok := features["openAgent"].(map[interface{}]interface{}); ok {
				if minimumInterval, ok := openAgent["minimumInterval"].(string); ok {
					return minimumInterval
				}
			}
		}
	}
	// Default to 1s if minimumInterval is not set
	return "1s"
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
