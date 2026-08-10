package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v2"

	"open-agent/pkg/k8s"
	"open-agent/tools/util/logutil"

	"github.com/whatap/golib/util/fileutil"
)

const (
	// ScrapeConfigKey is the file name of the scrape configuration, and also the
	// ConfigMap data key that holds it.
	ScrapeConfigKey = "scrape_config.yaml"
	// DefaultConfigMapName is the ConfigMap holding the scrape configuration.
	DefaultConfigMapName = "whatap-open-agent-config"
	// DefaultConfigMapNamespace is used when the pod namespace cannot be detected.
	DefaultConfigMapNamespace = "whatap-monitoring"
)

// Source identifiers reported back to the server so it can tell where the
// configuration came from, and whether it is writable.
const (
	ScrapeConfigSourceConfigMap = "configmap"
	ScrapeConfigSourceFile      = "file"
)

// ResolveConfigMapNamespace returns the namespace holding the scrape ConfigMap.
func ResolveConfigMapNamespace() string {
	if namespace := getPodNamespace(); namespace != "" {
		return namespace
	}
	return DefaultConfigMapNamespace
}

// ScrapeConfigFilePath returns the path of the local scrape configuration file.
func ScrapeConfigFilePath() string {
	homeDir := os.Getenv("WHATAP_OPEN_HOME")
	if homeDir == "" {
		homeDir = "."
	}
	return filepath.Join(homeDir, ScrapeConfigKey)
}

// activeScrapeConfigSource reports which source ConfigManager.LoadConfig would
// read from. It must stay in sync with the branching in LoadConfig.
func activeScrapeConfigSource() string {
	if forceStandaloneMode {
		return ScrapeConfigSourceFile
	}
	if client := k8s.GetInstance(); client != nil && client.IsInitialized() {
		return ScrapeConfigSourceConfigMap
	}
	return ScrapeConfigSourceFile
}

// ScrapeConfigWritable reports whether WriteScrapeConfigRaw can replace the
// configuration for the currently active source.
//
// Reported to the server alongside the contents so the caller can tell upfront
// whether an edit is possible, instead of inferring it from the source name or
// discovering it only after a rejected write.
func ScrapeConfigWritable() bool {
	return activeScrapeConfigSource() == ScrapeConfigSourceFile
}

// ReadScrapeConfigRaw returns the scrape configuration as raw YAML text along
// with the source it was read from.
//
// The raw text is returned rather than a re-marshalled version of the parsed
// configuration so that comments, key ordering and indentation are preserved.
func ReadScrapeConfigRaw() (contents string, source string, err error) {
	source = activeScrapeConfigSource()

	if source == ScrapeConfigSourceConfigMap {
		namespace := ResolveConfigMapNamespace()
		configMap, err := k8s.GetInstance().GetConfigMap(namespace, DefaultConfigMapName)
		if err != nil || configMap == nil {
			return "", source, fmt.Errorf("ConfigMap %s/%s not found: %v", namespace, DefaultConfigMapName, err)
		}
		data, ok := configMap.Data[ScrapeConfigKey]
		if !ok {
			return "", source, fmt.Errorf("%s not found in ConfigMap %s/%s", ScrapeConfigKey, namespace, DefaultConfigMapName)
		}
		return data, source, nil
	}

	path := ScrapeConfigFilePath()
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		return "", source, fmt.Errorf("error reading configuration file %s: %v", path, readErr)
	}
	return string(data), source, nil
}

// WriteScrapeConfigRaw validates and replaces the scrape configuration,
// returning the source it targeted.
//
// Writing is only supported when the local file is the active source. In a
// Kubernetes environment the ConfigMap owns the configuration - writing the
// local file there would silently do nothing, because LoadConfig reads the
// ConfigMap first.
func WriteScrapeConfigRaw(contents string) (source string, err error) {
	source = activeScrapeConfigSource()

	// Validate before touching the file: an unparsable scrape configuration
	// makes LoadConfig fail and drops every scrape target.
	if err := ValidateScrapeConfig(contents); err != nil {
		return source, err
	}

	// The error is surfaced to the user through the control response, so it has
	// to say what to do instead - not just that the write was refused.
	// Kept in sync with ScrapeConfigWritable so the advertised capability and the
	// actual refusal can never disagree.
	if !ScrapeConfigWritable() {
		return source, fmt.Errorf(
			"the scrape configuration is read from ConfigMap %s/%s in a Kubernetes environment "+
				"and cannot be updated by the agent. Edit spec.features.openAgent in the WhatapAgent CR "+
				"(the operator regenerates the ConfigMap), or run the agent in standalone mode "+
				"to manage %s directly",
			ResolveConfigMapNamespace(), DefaultConfigMapName, ScrapeConfigKey)
	}

	// ReplaceFileWithOldBackup writes via a temp file and keeps the previous
	// contents as <path>.old. The resulting mtime change is what the
	// ConfigManager file watcher picks up, so no explicit reload is needed.
	path := ScrapeConfigFilePath()
	if err := fileutil.ReplaceFileWithOldBackup(path, contents); err != nil {
		return source, fmt.Errorf("error writing configuration file %s: %v", path, err)
	}

	logutil.Infof("CONFIG", "Scrape configuration replaced: %s (backup: %s.old)", path, path)
	return source, nil
}

// ValidateScrapeConfig checks that the contents parse as YAML and carry the
// minimum structure GetScrapeConfigs expects.
func ValidateScrapeConfig(contents string) error {
	var parsed map[string]interface{}
	if err := yaml.Unmarshal([]byte(contents), &parsed); err != nil {
		return fmt.Errorf("invalid yaml: %v", err)
	}
	if parsed == nil {
		return fmt.Errorf("invalid yaml: empty document")
	}

	// yaml.v2 decodes nested mappings as map[interface{}]interface{}, matching
	// the type assertions used throughout ConfigManager.
	features, ok := parsed["features"].(map[interface{}]interface{})
	if !ok {
		return fmt.Errorf("missing 'features' section")
	}
	if _, ok := features["openAgent"].(map[interface{}]interface{}); !ok {
		return fmt.Errorf("missing 'features.openAgent' section")
	}
	return nil
}
