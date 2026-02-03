package scraper

import (
	"open-agent/pkg/config"
	"open-agent/pkg/discovery"
	"sync"
	"testing"
)

func TestStartTargetSchedulerRace(t *testing.T) {
	// Initialize ScraperManager
	sm := &ScraperManager{
		targetSchedulers: make(map[string]*TargetScheduler),
		configManager:    &config.ConfigManager{}, // Zero value, assuming methods are safe
	}

	target := &discovery.Target{
		ID: "test-race-target",
		Metadata: map[string]interface{}{
			"endpoint": discovery.EndpointConfig{},
		},
	}

	// Concurrency level
	concurrency := 20
	var wg sync.WaitGroup

	// Try to start scheduler concurrently
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sm.startTargetScheduler(target)
		}()
	}

	wg.Wait()

	// Verify map contains exactly one entry
	sm.schedulerMutex.RLock()
	count := len(sm.targetSchedulers)
	scheduler := sm.targetSchedulers[target.ID]
	sm.schedulerMutex.RUnlock()

	if count != 1 {
		t.Errorf("Expected exactly 1 scheduler, found %d", count)
	}

	if scheduler == nil {
		t.Errorf("Scheduler for target ID not found in map")
	}

	// Clean up
	sm.stopAllSchedulers()
}
