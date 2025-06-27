package discovery

import (
	"context"
	"testing"
	"time"

	"open-agent/pkg/config"
)

func TestKubernetesDiscovery_LoadTargets(t *testing.T) {
	// Create a mock config manager
	configManager := &config.ConfigManager{}
	
	// Create discovery instance
	discovery := NewKubernetesDiscovery(configManager)
	
	// Test target configuration
	targets := []map[string]interface{}{
		{
			"targetName": "test-target",
			"type":       "PodMonitor",
			"enabled":    true,
			"namespaceSelector": map[string]interface{}{
				"matchNames": []interface{}{"default"},
			},
			"selector": map[string]interface{}{
				"matchLabels": map[string]interface{}{
					"app": "test-app",
				},
			},
			"endpoints": []interface{}{
				map[string]interface{}{
					"port": "metrics",
					"path": "/metrics",
				},
			},
		},
	}
	
	// Test loading targets
	err := discovery.LoadTargets(targets)
	if err != nil {
		t.Fatalf("Failed to load targets: %v", err)
	}
	
	// Verify configuration was loaded
	if len(discovery.configs) != 1 {
		t.Fatalf("Expected 1 config, got %d", len(discovery.configs))
	}
	
	config := discovery.configs[0]
	if config.TargetName != "test-target" {
		t.Errorf("Expected target name 'test-target', got '%s'", config.TargetName)
	}
	
	if config.Type != "PodMonitor" {
		t.Errorf("Expected type 'PodMonitor', got '%s'", config.Type)
	}
	
	if !config.Enabled {
		t.Error("Expected target to be enabled")
	}
}

func TestKubernetesDiscovery_DisabledTarget(t *testing.T) {
	// Create a mock config manager
	configManager := &config.ConfigManager{}
	
	// Create discovery instance
	discovery := NewKubernetesDiscovery(configManager)
	
	// Test disabled target configuration
	targets := []map[string]interface{}{
		{
			"targetName": "disabled-target",
			"type":       "PodMonitor",
			"enabled":    false,
		},
	}
	
	// Test loading targets
	err := discovery.LoadTargets(targets)
	if err != nil {
		t.Fatalf("Failed to load targets: %v", err)
	}
	
	// Verify disabled target was not loaded
	if len(discovery.configs) != 0 {
		t.Fatalf("Expected 0 configs for disabled target, got %d", len(discovery.configs))
	}
}

func TestKubernetesDiscovery_StartStop(t *testing.T) {
	// Create a mock config manager
	configManager := &config.ConfigManager{}
	
	// Create discovery instance
	discovery := NewKubernetesDiscovery(configManager)
	
	// Test starting discovery
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	
	err := discovery.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start discovery: %v", err)
	}
	
	// Test stopping discovery
	err = discovery.Stop()
	if err != nil {
		t.Fatalf("Failed to stop discovery: %v", err)
	}
}

func TestTarget_StateManagement(t *testing.T) {
	target := &Target{
		ID:    "test-target-1",
		URL:   "http://localhost:8080/metrics",
		State: TargetStatePending,
		Labels: map[string]string{
			"job": "test-job",
		},
		Metadata: map[string]interface{}{
			"targetName": "test",
		},
		LastSeen: time.Now(),
	}
	
	// Test initial state
	if target.State != TargetStatePending {
		t.Errorf("Expected initial state to be Pending, got %s", target.State)
	}
	
	// Test state transition
	target.State = TargetStateReady
	if target.State != TargetStateReady {
		t.Errorf("Expected state to be Ready, got %s", target.State)
	}
}