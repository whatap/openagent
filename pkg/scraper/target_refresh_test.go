package scraper

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"open-agent/pkg/config"
	"open-agent/pkg/discovery"
	"open-agent/pkg/k8s"
	"open-agent/pkg/model"
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

func TestMain(m *testing.M) {
	// HTTP scraper tests must never initialize a developer's Kubernetes client.
	k8s.SetStandaloneMode(true)
	os.Exit(m.Run())
}

// readyTargetDiscovery supplies successive discovery snapshots without a cluster.
type readyTargetDiscovery struct {
	targets []*discovery.Target
}

func (d *readyTargetDiscovery) LoadTargets([]map[string]interface{}) error { return nil }
func (d *readyTargetDiscovery) Start(context.Context) error                { return nil }
func (d *readyTargetDiscovery) Stop() error                                { return nil }
func (d *readyTargetDiscovery) GetReadyTargets() []*discovery.Target       { return d.targets }

func newRefreshTestTarget(targetURL string) *discovery.Target {
	return &discovery.Target{
		ID:     "dcgm/gpu-operator/dcgm-exporter/9400-metrics",
		URL:    targetURL,
		Labels: map[string]string{"instance": targetURL},
		State:  discovery.TargetStateReady,
		Metadata: map[string]interface{}{
			"targetName": "dcgm",
			"type":       "PodMonitor",
			"endpoint": discovery.EndpointConfig{
				Port: "9400", Path: "/metrics", Scheme: "http", Interval: "60s", Timeout: "1s",
			},
		},
	}
}

func TestUpdateTargetSchedulers_ScrapesChangedAddress(t *testing.T) {
	var oldRequests, newRequests atomic.Int64
	oldServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		oldRequests.Add(1)
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		_, _ = w.Write([]byte("dcgm_test_metric 1\n"))
	}))
	defer oldServer.Close()
	newServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		newRequests.Add(1)
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		_, _ = w.Write([]byte("dcgm_test_metric 2\n"))
	}))
	defer newServer.Close()

	oldTarget := newRefreshTestTarget(oldServer.URL + "/metrics")
	newTarget := newRefreshTestTarget(newServer.URL + "/metrics")
	d := &readyTargetDiscovery{targets: []*discovery.Target{oldTarget}}
	queue := make(chan *model.ScrapeRawData, 1)
	sm := NewScraperManager(&config.ConfigManager{}, d, queue)
	defer sm.stopAllSchedulers()
	sm.updateTargetSchedulers()
	scheduler := sm.targetSchedulers[oldTarget.ID]

	scrape := func() *model.ScrapeRawData {
		t.Helper()
		sm.scrapeTarget(scheduler.getTarget())
		select {
		case data := <-queue:
			return data
		default:
			t.Fatal("scrape did not produce raw data")
			return nil
		}
	}
	if data := scrape(); data.TargetURL != oldTarget.URL {
		t.Fatalf("initial scrape URL = %q, want %q", data.TargetURL, oldTarget.URL)
	}

	// The pod identity and endpoint config stay the same; only its address changes.
	d.targets = []*discovery.Target{newTarget}
	sm.updateTargetSchedulers()
	if sm.targetSchedulers[newTarget.ID] != scheduler {
		t.Fatal("address refresh restarted the scheduler")
	}
	data := scrape()
	if data.TargetURL != newTarget.URL || data.RawData != "dcgm_test_metric 2\n" {
		t.Errorf("scraped stale target: URL=%q data=%q; want URL=%q", data.TargetURL, data.RawData, newTarget.URL)
	}
	if data.Labels["instance"] != newTarget.Labels["instance"] {
		t.Errorf("instance label = %q, want %q", data.Labels["instance"], newTarget.Labels["instance"])
	}
	if oldRequests.Load() != 1 || newRequests.Load() != 1 {
		t.Errorf("HTTP requests after refresh: old=%d new=%d, want one each", oldRequests.Load(), newRequests.Load())
	}
}

func TestTargetScheduler_RefreshDuringAdaptiveTimeout(t *testing.T) {
	const refreshIterations = 128
	targets := []*discovery.Target{
		newRefreshTestTarget("http://192.0.2.10:9400/metrics"),
		newRefreshTestTarget("http://192.0.2.11:9400/metrics"),
	}
	scheduler := &TargetScheduler{
		target:                 targets[0],
		adaptiveTimeoutEnabled: true,
		failureThreshold:       1,
		timeoutMultiplier:      2,
		baseTimeout:            time.Second,
		currentTimeout:         time.Second,
		maxTimeout:             4 * time.Second,
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < refreshIterations; i++ {
			scheduler.updateTarget(targets[i%len(targets)])
		}
	}()
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < refreshIterations; i++ {
			scheduler.increaseTimeout()
			scheduler.resetTimeout()
			if got := scheduler.getCurrentTimeout(); got != time.Second {
				t.Errorf("timeout after reset = %v, want 1s", got)
			}
		}
	}()
	close(start)
	wg.Wait()
}

func TestUpdateTargetSchedulers_PreservesSchedulerOnRefresh(t *testing.T) {
	tests := []struct {
		name   string
		change func(*discovery.Target)
	}{
		{name: "labels_only", change: func(target *discovery.Target) {
			target.Labels["node"] = "gpu-worker-b"
		}},
		{name: "metadata_only", change: func(target *discovery.Target) {
			target.Metadata["addNodeLabel"] = true
		}},
		{name: "endpoint_config", change: func(target *discovery.Target) {
			endpoint := target.Metadata["endpoint"].(discovery.EndpointConfig)
			endpoint.TLSConfig = map[string]interface{}{"serverName": "exporter.example.test"}
			target.Metadata["endpoint"] = endpoint
		}},
		{name: "unchanged_snapshot", change: func(target *discovery.Target) {
			target.LastSeen = time.Unix(2, 0)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldTarget := newRefreshTestTarget("http://192.0.2.10:9400/metrics")
			newTarget := newRefreshTestTarget(oldTarget.URL)
			tt.change(newTarget)
			d := &readyTargetDiscovery{targets: []*discovery.Target{oldTarget}}
			sm := NewScraperManager(&config.ConfigManager{}, d, nil)
			defer sm.stopAllSchedulers()
			sm.updateTargetSchedulers()
			scheduler := sm.targetSchedulers[oldTarget.ID]
			ticker := scheduler.ticker
			if !scheduler.tryStartScraping() {
				t.Fatal("could not start initial scrape")
			}

			d.targets = []*discovery.Target{newTarget}
			sm.updateTargetSchedulers()
			if len(sm.targetSchedulers) != 1 || sm.targetSchedulers[newTarget.ID] != scheduler || scheduler.ticker != ticker {
				t.Fatal("refresh must preserve the single scheduler and its ticker")
			}
			if scheduler.getTarget() != newTarget {
				t.Error("scheduler did not adopt the latest discovery snapshot")
			}
			if scheduler.tryStartScraping() {
				t.Error("refresh allowed an overlapping scrape")
			}
			scheduler.finishScraping()
			if !scheduler.tryStartScraping() {
				t.Error("refresh prevented the next scrape after completion")
			}
			scheduler.finishScraping()
		})
	}
}

func TestUpdateTargetSchedulers_RestartsOnIntervalChange(t *testing.T) {
	oldTarget := newRefreshTestTarget("http://192.0.2.10:9400/metrics")
	newTarget := newRefreshTestTarget("http://192.0.2.11:9400/metrics")
	endpoint := newTarget.Metadata["endpoint"].(discovery.EndpointConfig)
	endpoint.Interval = "120s"
	newTarget.Metadata["endpoint"] = endpoint
	d := &readyTargetDiscovery{targets: []*discovery.Target{oldTarget}}
	sm := NewScraperManager(&config.ConfigManager{}, d, nil)
	defer sm.stopAllSchedulers()
	sm.updateTargetSchedulers()
	oldScheduler := sm.targetSchedulers[oldTarget.ID]

	d.targets = []*discovery.Target{newTarget}
	sm.updateTargetSchedulers()
	newScheduler := sm.targetSchedulers[newTarget.ID]
	if newScheduler == oldScheduler || newScheduler.getTarget() != newTarget || newScheduler.interval != 2*time.Minute {
		t.Error("interval change did not restart the scheduler with the latest target")
	}
	select {
	case <-oldScheduler.stopCh:
	default:
		t.Error("old scheduler was not stopped")
	}
}

func TestUpdateTargetSchedulers_RemovesMissingTarget(t *testing.T) {
	target := newRefreshTestTarget("http://192.0.2.10:9400/metrics")
	d := &readyTargetDiscovery{targets: []*discovery.Target{target}}
	sm := NewScraperManager(&config.ConfigManager{}, d, nil)
	defer sm.stopAllSchedulers()
	sm.updateTargetSchedulers()
	scheduler := sm.targetSchedulers[target.ID]

	// GetReadyTargets excludes deleted and unready pods.
	d.targets = nil
	sm.updateTargetSchedulers()
	if len(sm.targetSchedulers) != 0 {
		t.Error("scheduler for the missing target was retained")
	}
	select {
	case <-scheduler.stopCh:
	default:
		t.Error("scheduler for the missing target was not stopped")
	}
}
