package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"open-agent/tools/util/logutil"

	golibconfig "github.com/whatap/golib/config"
)

// singleton instance of WhatapConfig
// This allows direct access to configuration values without explicitly creating a WhatapConfig instance.
// Example: whatap_config.GetConfig().Debug
var instance *WhatapConfig
var once sync.Once

// init initializes the singleton instance of WhatapConfig when the package is loaded.
// This ensures that the configuration is loaded and the watcher is started automatically.
func init() {
	once.Do(func() {
		instance = NewWhatapConfig()
	})
}

// GetInstance returns the singleton WhatapConfig instance.
func GetInstance() *WhatapConfig {
	return instance
}

// GetConfig returns the configuration as a Config struct from the singleton instance.
// This function can be called directly without creating a WhatapConfig instance.
func GetConfig() *Config {
	return instance.GetConfig()
}

// IsDebugEnabled returns true if debug is enabled in the configuration from the singleton instance.
// This function can be called directly without creating a WhatapConfig instance.
func IsDebugEnabled() bool {
	return instance.IsDebugEnabled()
}

// Get returns the value for the given key from the singleton instance.
// This function can be called directly without creating a WhatapConfig instance.
func Get(key string) string {
	return instance.Get(key)
}

// GetWithDefault returns the value for the given key, or the default value if the key is not found from the singleton instance.
// This function can be called directly without creating a WhatapConfig instance.
func GetWithDefault(key, defaultValue string) string {
	return instance.GetWithDefault(key, defaultValue)
}

// GetBool returns the boolean value for the given key from the singleton instance.
// This function can be called directly without creating a WhatapConfig instance.
func GetBool(key string) bool {
	return instance.GetBool(key)
}

// GetBoolWithDefault returns the boolean value for the given key, or the default value if the key is not found from the singleton instance.
// This function can be called directly without creating a WhatapConfig instance.
func GetBoolWithDefault(key string, defaultValue bool) bool {
	return instance.GetBoolWithDefault(key, defaultValue)
}

// GetIntValue returns the integer value for the given key from the singleton instance.
// This function can be called directly without creating a WhatapConfig instance.
func GetIntValue(key string) int {
	return int(instance.GetInt(key, 0))
}

// GetIntWithDefault returns the integer value for the given key, or the default value if the key is not found from the singleton instance.
// This function can be called directly without creating a WhatapConfig instance.
func GetIntWithDefault(key string, defaultValue int) int {
	return instance.GetIntWithDefault(key, defaultValue)
}

// GetConfigMap returns the entire configuration as a map from the singleton instance.
// This function can be called directly without creating a WhatapConfig instance.
func GetConfigMap() map[string]string {
	return instance.GetConfigMap()
}

// Cleanup stops the configuration watcher of the singleton instance.
// This function should be called when the application is shutting down to clean up resources.
func Cleanup() {
	if instance != nil {
		instance.Stop()
	}
}

// WhatapConfig represents the configuration from whatap.conf
// It supports dynamic reloading of the configuration file when changes are detected.
type WhatapConfig struct {
	// Map to store configuration key-value pairs
	values map[string]string
	// Path to the configuration file
	configFile string
	// Mutex for thread-safe access to the values map
	mu sync.RWMutex
	// Channel for stopping the configuration watcher
	stopCh chan struct{}
}

// NewWhatapConfig creates a new WhatapConfig instance and loads the configuration.
// It also starts a background goroutine to watch for changes to the configuration file.
func NewWhatapConfig() *WhatapConfig {
	wc := &WhatapConfig{
		values: make(map[string]string),
		stopCh: make(chan struct{}),
	}
	wc.LoadConfig()
	// Start watching for changes to the configuration file
	go wc.watchConfig()
	return wc
}

// LoadConfig loads the configuration from whatap.conf.
// It reads the configuration file and updates the values map in a thread-safe manner.
// This method is called during initialization and whenever the configuration file changes.
func (wc *WhatapConfig) LoadConfig() error {
	// Get the home directory from environment variable or use current directory
	homeDir := os.Getenv("WHATAP_HOME")
	if homeDir == "" {
		homeDir = "."
	}

	// Path to whatap.conf
	configFile := filepath.Join(homeDir, "whatap.conf")
	wc.configFile = configFile

	// Check if the file exists
	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		// File doesn't exist, create an empty one
		file, err := os.Create(configFile)
		if err != nil {
			return err
		}
		file.Close()
		return nil
	}

	// Open the file
	file, err := os.Open(configFile)
	if err != nil {
		return err
	}
	defer file.Close()

	// Create a new map for the values
	newValues := make(map[string]string)

	// Read the file line by line
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Split the line into key and value
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		// Trim spaces and store the key-value pair
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		newValues[key] = value
	}

	// Update the values map with the lock held
	wc.mu.Lock()
	wc.values = newValues
	wc.mu.Unlock()

	return scanner.Err()
}

// Get returns the value for the given key.
// This method is thread-safe and can be called concurrently from multiple goroutines.
// It uses a read lock to ensure that the values map is not modified while being read.
// It first checks if the key exists in the configuration file, and if it does, returns that value.
// If the key is not found in the configuration file, it checks for an environment variable with the same name.
func (wc *WhatapConfig) Get(key string) string {
	wc.mu.RLock()
	defer wc.mu.RUnlock()

	// First check if the key exists in the configuration file
	if value, exists := wc.values[key]; exists && value != "" {
		return value
	}

	// If not found in the configuration file, check for an environment variable
	envValue := os.Getenv(key)
	return envValue
}

// GetWithDefault returns the value for the given key, or the default value if the key is not found.
// This method is thread-safe as it uses the Get method which is thread-safe.
func (wc *WhatapConfig) GetWithDefault(key, defaultValue string) string {
	value := wc.Get(key)
	if value == "" {
		return defaultValue
	}
	return value
}

// GetBool returns the boolean value for the given key.
// It returns true if the value is "true", "yes", or "1".
// This method is thread-safe as it uses the Get method which is thread-safe.
func (wc *WhatapConfig) GetBool(key string) bool {
	value := wc.Get(key)
	return value == "true" || value == "yes" || value == "1"
}

// GetBoolWithDefault returns the boolean value for the given key, or the default value if the key is not found.
// This method is thread-safe as it uses the Get method which is thread-safe.
func (wc *WhatapConfig) GetBoolWithDefault(key string, defaultValue bool) bool {
	value := wc.Get(key)
	if value == "" {
		return defaultValue
	}
	return value == "true" || value == "yes" || value == "1"
}

// Config represents the configuration values from whatap.conf as a struct.
// This allows for dot notation access to configuration values.
type Config struct {
	WHATAP_LICENSE string
	WHATAP_HOST    string
	WHATAP_PORT    string
	Debug          bool
	ScrapeInterval string
	ScrapeTimeout  string
	ServerPort     int
	ServerHost     string
	LogLevel       string
	LogFile        string
	EnableMetrics  bool
	EnableTracing  bool
	EnableLogging  bool
	// Add other configuration fields as needed
}

// GetConfig returns the configuration as a Config struct.
// This method is thread-safe and can be called concurrently from multiple goroutines.
// It uses the Get method to retrieve values from the configuration file.
func (wc *WhatapConfig) GetConfig() *Config {
	// Helper function to parse an integer value with a default
	parseIntValue := func(value string, defaultValue int) int {
		if value == "" {
			return defaultValue
		}
		intValue, err := strconv.Atoi(value)
		if err != nil {
			logutil.Infof("CONFIG", "Error converting %s to integer: %v", value, err)
			return defaultValue
		}
		return intValue
	}

	// Helper function to check if a value is truthy
	isTruthy := func(value string) bool {
		return value == "true" || value == "yes" || value == "1"
	}

	config := &Config{
		WHATAP_LICENSE: wc.Get("WHATAP_LICENSE"),
		WHATAP_HOST:    wc.Get("WHATAP_HOST"),
		WHATAP_PORT:    wc.Get("WHATAP_PORT"),
		Debug:          isTruthy(wc.Get("debug")),
		ScrapeInterval: wc.Get("scrape_interval"),
		ScrapeTimeout:  wc.Get("scrape_timeout"),
		ServerPort:     parseIntValue(wc.Get("server_port"), 0),
		ServerHost:     wc.Get("server_host"),
		LogLevel:       wc.Get("log_level"),
		LogFile:        wc.Get("log_file"),
		EnableMetrics:  isTruthy(wc.Get("enable_metrics")),
		EnableTracing:  isTruthy(wc.Get("enable_tracing")),
		EnableLogging:  isTruthy(wc.Get("enable_logging")),
	}

	return config
}

// GetConfigMap returns the entire configuration as a map.
// This method is thread-safe and can be called concurrently from multiple goroutines.
func (wc *WhatapConfig) GetConfigMap() map[string]string {
	wc.mu.RLock()
	defer wc.mu.RUnlock()

	// Create a copy of the values map to avoid concurrent modification issues
	configCopy := make(map[string]string, len(wc.values))
	for k, v := range wc.values {
		configCopy[k] = v
	}

	return configCopy
}

// GetIntWithDefault returns the integer value for the given key, or the default value if the key is not found or cannot be converted to an integer.
// This method is thread-safe as it uses the Get method which is thread-safe.
func (wc *WhatapConfig) GetIntWithDefault(key string, defaultValue int) int {
	return int(wc.GetInt(key, defaultValue))
}

// IsDebugEnabled returns true if debug is enabled in the configuration.
// This is a convenience method that checks if the "debug" key is set to a truthy value.
func (wc *WhatapConfig) IsDebugEnabled() bool {
	return wc.GetBool("debug")
}

// watchConfig periodically checks for changes to the configuration file.
// It runs in a separate goroutine and checks the file's modification time every 5 seconds.
// If the file has been modified, it reloads the configuration.
// This allows for dynamic detection and application of configuration changes without restarting the application.
func (wc *WhatapConfig) watchConfig() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	var lastModTime time.Time
	for {
		select {
		case <-ticker.C:
			// Check if the file has been modified
			fileInfo, err := os.Stat(wc.configFile)
			if err != nil {
				logutil.Infof("CONFIG", "Error checking config file: %v", err)
				continue
			}

			// If the file has been modified since the last check, reload it
			if fileInfo.ModTime().After(lastModTime) {
				logutil.Infof("CONFIG", "Config file changed, reloading...")
				lastModTime = fileInfo.ModTime()
				if err := wc.LoadConfig(); err != nil {
					logutil.Infof("CONFIG", "Error reloading config: %v", err)
				} else {
					golibconfig.GetConfigObserver().Run(wc)
				}
			}
		case <-wc.stopCh:
			return
		}
	}
}

// golib/config.Config interface implementation for WhatapConfig

func (wc *WhatapConfig) ApplyDefault() {}

func (wc *WhatapConfig) GetConfFile() string {
	return wc.configFile
}

func (wc *WhatapConfig) Destroy() {}

func (wc *WhatapConfig) GetKeys() []string {
	wc.mu.RLock()
	defer wc.mu.RUnlock()
	keys := make([]string, 0, len(wc.values))
	for k := range wc.values {
		keys = append(keys, k)
	}
	return keys
}

func (wc *WhatapConfig) GetValue(key string) string {
	return wc.Get(key)
}

func (wc *WhatapConfig) GetValueDef(key, def string) string {
	return wc.GetWithDefault(key, def)
}

func (wc *WhatapConfig) GetBoolean(key string, def bool) bool {
	return wc.GetBoolWithDefault(key, def)
}

func (wc *WhatapConfig) GetInt(key string, def int) int32 {
	value := wc.Get(key)
	if value == "" {
		return int32(def)
	}
	intValue, err := strconv.Atoi(value)
	if err != nil {
		return int32(def)
	}
	return int32(intValue)
}

func (wc *WhatapConfig) GetIntSet(key, def, deli string) []int32 {
	value := wc.GetWithDefault(key, def)
	parts := strings.Split(value, deli)
	result := make([]int32, 0, len(parts))
	for _, p := range parts {
		if v, err := strconv.Atoi(strings.TrimSpace(p)); err == nil {
			result = append(result, int32(v))
		}
	}
	return result
}

func (wc *WhatapConfig) GetLong(key string, def int64) int64 {
	value := wc.Get(key)
	if value == "" {
		return def
	}
	longValue, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return def
	}
	return longValue
}

func (wc *WhatapConfig) GetStringArray(key string, def string, deli string) []string {
	value := wc.GetWithDefault(key, def)
	parts := strings.Split(value, deli)
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func (wc *WhatapConfig) GetStringHashSet(key, def, deli string) []int32 {
	return wc.GetIntSet(key, def, deli)
}

func (wc *WhatapConfig) GetStringHashCodeSet(key, def, deli string) []int32 {
	return wc.GetIntSet(key, def, deli)
}

func (wc *WhatapConfig) GetFloat(key string, def float32) float32 {
	value := wc.Get(key)
	if value == "" {
		return def
	}
	floatValue, err := strconv.ParseFloat(value, 32)
	if err != nil {
		return def
	}
	return float32(floatValue)
}

func (wc *WhatapConfig) SetValues(v *map[string]string) {
	wc.mu.Lock()
	defer wc.mu.Unlock()
	wc.values = *v
}

func (wc *WhatapConfig) ToString() string {
	return fmt.Sprintf("WhatapConfig{configFile: %s}", wc.configFile)
}

func (wc *WhatapConfig) String() string {
	return wc.ToString()
}

// Stop stops the configuration watcher.
// This method should be called when the application is shutting down to clean up resources.
// It signals the watchConfig goroutine to stop and releases associated resources.
func (wc *WhatapConfig) Stop() {
	close(wc.stopCh)
}
