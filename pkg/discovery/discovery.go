package discovery

import (
	"context"
	"time"

	"open-agent/pkg/config"
	"open-agent/pkg/model"
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
	RelabelConfigs    model.RelabelConfigs
}

// AdaptiveTimeoutConfig represents adaptive timeout configuration
type AdaptiveTimeoutConfig struct {
	Enabled          bool    // Enable adaptive timeout (default: true)
	FailureThreshold int     // Number of consecutive failures before increasing timeout (default: 2)
	Multiplier       float64 // Timeout multiplier on failure (default: 2.0)
}

// EndpointConfig represents endpoint configuration
type EndpointConfig struct {
	Port                 string // For PodMonitor/ServiceMonitor
	Address              string // For StaticEndpoints
	Path                 string
	Scheme               string
	Interval             string
	Timeout              string                 // HTTP request timeout (e.g., "10s", "1m")
	AdaptiveTimeout      *AdaptiveTimeoutConfig // Adaptive timeout configuration
	TLSConfig            map[string]interface{}
	BasicAuth            *config.BasicAuthConfig
	MetricRelabelConfigs []interface{}
	Params               map[string]interface{} // HTTP URL parameters
	AddNodeLabel         bool
}
