package config

import (
	"bufio"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// WhatapConfig represents the configuration from whatap.conf
// It supports dynamic reloading of the configuration file when changes are detected.
type WhatapConfig struct {
	// Map to store configuration key-value pairs
	values     map[string]string
	// Path to the configuration file
	configFile string
	// Mutex for thread-safe access to the values map
	mu         sync.RWMutex
	// Channel for stopping the configuration watcher
	stopCh     chan struct{}
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
func (wc *WhatapConfig) Get(key string) string {
	wc.mu.RLock()
	defer wc.mu.RUnlock()
	return wc.values[key]
}

// GetBool returns the boolean value for the given key.
// It returns true if the value is "true", "yes", or "1".
// This method is thread-safe as it uses the Get method which is thread-safe.
func (wc *WhatapConfig) GetBool(key string) bool {
	value := wc.Get(key)
	return value == "true" || value == "yes" || value == "1"
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
				log.Printf("Error checking config file: %v", err)
				continue
			}

			// If the file has been modified since the last check, reload it
			if fileInfo.ModTime().After(lastModTime) {
				log.Printf("Config file changed, reloading...")
				lastModTime = fileInfo.ModTime()
				if err := wc.LoadConfig(); err != nil {
					log.Printf("Error reloading config: %v", err)
				}
			}
		case <-wc.stopCh:
			return
		}
	}
}

// Stop stops the configuration watcher.
// This method should be called when the application is shutting down to clean up resources.
// It signals the watchConfig goroutine to stop and releases associated resources.
func (wc *WhatapConfig) Stop() {
	close(wc.stopCh)
}
