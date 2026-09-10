package scraper

import (
	"testing"

	"open-agent/pkg/discovery"
)

func TestHasTargetChangedWhenPodIPChanges(t *testing.T) {
	sm := &ScraperManager{}
	oldTarget := &discovery.Target{
		ID:       "dcgm/default/dcgm-exporter/9400-metrics",
		URL:      "http://10.42.2.36:9400/metrics",
		Metadata: map[string]interface{}{"endpoint": discovery.EndpointConfig{Port: "9400", Path: "/metrics"}},
	}
	newTarget := &discovery.Target{
		ID:       oldTarget.ID,
		URL:      "http://10.42.2.15:9400/metrics",
		Metadata: map[string]interface{}{"endpoint": discovery.EndpointConfig{Port: "9400", Path: "/metrics"}},
	}

	if !sm.hasTargetChanged(oldTarget, newTarget) {
		t.Fatal("expected Pod IP URL change to refresh the scheduler target")
	}
}

func TestHasTargetChangedReturnsFalseForUnchangedTarget(t *testing.T) {
	sm := &ScraperManager{}
	endpoint := discovery.EndpointConfig{Port: "9400", Path: "/metrics"}
	oldTarget := &discovery.Target{
		ID:       "dcgm/default/dcgm-exporter/9400-metrics",
		URL:      "http://10.42.2.15:9400/metrics",
		Metadata: map[string]interface{}{"endpoint": endpoint},
	}
	newTarget := &discovery.Target{
		ID:       oldTarget.ID,
		URL:      oldTarget.URL,
		Metadata: map[string]interface{}{"endpoint": endpoint},
	}

	if sm.hasTargetChanged(oldTarget, newTarget) {
		t.Fatal("did not expect an unchanged target to refresh the scheduler")
	}
}
