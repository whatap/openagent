package discovery

import (
	"context"
	"time"
)

// Target represents a discovered scrape target
type Target struct {
	ID       string                 // Unique identifier
	URL      string                 // Scraping URL
	Labels   map[string]string      // Metadata labels
	Metadata map[string]interface{} // Additional metadata

	// State information
	State      TargetState
	LastSeen   time.Time
	RetryCount int
}

type TargetState string

const (
	TargetStateReady   TargetState = "ready"
	TargetStatePending TargetState = "pending"
	TargetStateError   TargetState = "error"
	TargetStateRemoved TargetState = "removed"
)

// ServiceDiscovery interface for target discovery
type ServiceDiscovery interface {
	// Load targets from configuration
	LoadTargets(targets []map[string]interface{}) error

	// Start target discovery
	Start(ctx context.Context) error

	// Get currently ready targets
	GetReadyTargets() []*Target

	// Stop discovery
	Stop() error
}

// DiscoveryConfig represents configuration for a single target
type DiscoveryConfig struct {
	TargetName        string
	Type              string // "PodMonitor", "ServiceMonitor", "StaticEndpoints"
	Enabled           bool
	NamespaceSelector map[string]interface{}
	Selector          map[string]interface{}
	Endpoints         []EndpointConfig
}

// EndpointConfig represents endpoint configuration
type EndpointConfig struct {
	Port                 string // For PodMonitor/ServiceMonitor
	Address              string // For StaticEndpoints
	Path                 string
	Scheme               string
	Interval             string
	TLSConfig            map[string]interface{}
	MetricRelabelConfigs []interface{}
	Params               map[string]interface{} // HTTP URL parameters
	AddNodeLabel         bool
}
