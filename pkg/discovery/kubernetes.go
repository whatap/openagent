package discovery

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"open-agent/pkg/config"
	"open-agent/pkg/k8s"
	"open-agent/tools/util/logutil"
)

// KubernetesDiscovery implements ServiceDiscovery for Kubernetes environments
type KubernetesDiscovery struct {
	configManager *config.ConfigManager
	k8sClient     *k8s.K8sClient
	configs       []DiscoveryConfig
	targets       map[string]*Target
	targetsMutex  sync.RWMutex
	stopCh        chan struct{}
	ctx           context.Context
	cancel        context.CancelFunc
}

// NewKubernetesDiscovery creates a new KubernetesDiscovery instance
func NewKubernetesDiscovery(configManager *config.ConfigManager) *KubernetesDiscovery {
	return &KubernetesDiscovery{
		configManager: configManager,
		k8sClient:     k8s.GetInstance(),
		targets:       make(map[string]*Target),
		stopCh:        make(chan struct{}),
	}
}

// LoadTargets loads target configurations
func (kd *KubernetesDiscovery) LoadTargets(targets []map[string]interface{}) error {
	kd.configs = make([]DiscoveryConfig, 0, len(targets))

	for _, targetConfig := range targets {
		config, err := kd.parseDiscoveryConfig(targetConfig)
		if err != nil {
			logutil.Printf("ERROR", "Failed to parse target config: %v", err)
			continue
		}

		// Skip disabled targets
		if !config.Enabled {
			logutil.Printf("INFO", "Target %s is disabled, skipping", config.TargetName)
			continue
		}

		kd.configs = append(kd.configs, config)
	}

	logutil.Printf("INFO", "Loaded %d discovery configurations", len(kd.configs))
	return nil
}

// Start begins target discovery
func (kd *KubernetesDiscovery) Start(ctx context.Context) error {
	kd.ctx, kd.cancel = context.WithCancel(ctx)

	// Start periodic discovery
	go kd.discoveryLoop()

	logutil.Printf("INFO", "Kubernetes service discovery started")
	return nil
}

// GetReadyTargets returns all targets in ready state
func (kd *KubernetesDiscovery) GetReadyTargets() []*Target {
	kd.targetsMutex.RLock()
	defer kd.targetsMutex.RUnlock()

	var readyTargets []*Target
	for _, target := range kd.targets {
		if target.State == TargetStateReady {
			readyTargets = append(readyTargets, target)
		}
	}
	return readyTargets
}

// Stop stops the discovery process
func (kd *KubernetesDiscovery) Stop() error {
	if kd.cancel != nil {
		kd.cancel()
	}
	close(kd.stopCh)
	logutil.Printf("INFO", "Kubernetes service discovery stopped")
	return nil
}

// discoveryLoop runs the periodic target discovery
func (kd *KubernetesDiscovery) discoveryLoop() {
	// Initial discovery
	kd.discoverTargets()

	// Periodic discovery every 30 seconds (like Prometheus)
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			kd.discoverTargets()
		case <-kd.ctx.Done():
			return
		case <-kd.stopCh:
			return
		}
	}
}

// discoverTargets discovers all configured targets
func (kd *KubernetesDiscovery) discoverTargets() {
	for _, config := range kd.configs {
		switch config.Type {
		case "PodMonitor":
			kd.discoverPodTargets(config)
		case "ServiceMonitor":
			kd.discoverServiceTargets(config)
		case "StaticEndpoints":
			kd.discoverStaticTargets(config)
		default:
			logutil.Printf("WARN", "Unknown target type: %s", config.Type)
		}
	}
}

// discoverPodTargets discovers Pod-based targets
func (kd *KubernetesDiscovery) discoverPodTargets(config DiscoveryConfig) {
	if !kd.k8sClient.IsInitialized() {
		logutil.Printf("WARN", "Kubernetes client not initialized for PodMonitor: %s", config.TargetName)
		return
	}

	// Get matching namespaces
	namespaces, err := kd.getMatchingNamespaces(config.NamespaceSelector)
	if err != nil {
		logutil.Printf("ERROR", "Failed to get namespaces for %s: %v", config.TargetName, err)
		return
	}

	for _, namespace := range namespaces {
		// Get matching pods
		pods, err := kd.getMatchingPods(namespace, config.Selector)
		if err != nil {
			logutil.Printf("ERROR", "Failed to get pods for %s in namespace %s: %v", config.TargetName, namespace, err)
			continue
		}

		for _, pod := range pods {
			kd.processPodTarget(pod, config)
		}
	}
}

// processPodTarget processes a single pod target
func (kd *KubernetesDiscovery) processPodTarget(pod *corev1.Pod, config DiscoveryConfig) {
	// Check if pod is ready
	isReady := kd.isPodReady(pod)

	for _, endpoint := range config.Endpoints {
		targetID := fmt.Sprintf("%s-%s-%s-%s", config.TargetName, pod.Namespace, pod.Name, endpoint.Port)

		// Get pod IP
		podIP := pod.Status.PodIP
		if podIP == "" {
			logutil.Printf("DEBUG", "Pod %s/%s has no IP yet", pod.Namespace, pod.Name)
			continue
		}

		// Determine scheme
		scheme := kd.determineScheme(endpoint.Scheme, endpoint.Port, endpoint.TLSConfig)

		// Build target URL
		path := endpoint.Path
		if path == "" {
			path = config.Path
		}
		if path == "" {
			path = "/metrics"
		}

		url := fmt.Sprintf("%s://%s:%s%s", scheme, podIP, endpoint.Port, path)

		// Create or update target
		target := &Target{
			ID:  targetID,
			URL: url,
			Labels: map[string]string{
				"job":       config.TargetName,
				"namespace": pod.Namespace,
				"pod":       pod.Name,
			},
			Metadata: map[string]interface{}{
				"targetName":            config.TargetName,
				"type":                  config.Type,
				"endpoint":              endpoint,
				"metricRelabelConfigs":  endpoint.MetricRelabelConfigs,
				"addNodeLabel":          endpoint.AddNodeLabel,
			},
			LastSeen: time.Now(),
		}

		// Add node label if requested
		if endpoint.AddNodeLabel && pod.Spec.NodeName != "" {
			target.Labels["node"] = pod.Spec.NodeName
		}

		// Set target state based on pod readiness
		if isReady {
			target.State = TargetStateReady
		} else {
			target.State = TargetStatePending
			logutil.Printf("DEBUG", "Pod %s/%s is not ready yet", pod.Namespace, pod.Name)
		}

		kd.updateTarget(target)
	}
}

// isPodReady checks if a pod is ready (same logic as in ScraperManager)
func (kd *KubernetesDiscovery) isPodReady(pod *corev1.Pod) bool {
	// Check if the pod is in Running phase
	if pod.Status.Phase != corev1.PodRunning {
		return false
	}

	// Check if all containers are ready
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

// updateTarget updates or creates a target
func (kd *KubernetesDiscovery) updateTarget(newTarget *Target) {
	kd.targetsMutex.Lock()
	defer kd.targetsMutex.Unlock()

	existingTarget, exists := kd.targets[newTarget.ID]

	if !exists {
		// New target
		kd.targets[newTarget.ID] = newTarget
		logutil.Printf("DEBUG", "Added new target: %s (state: %s)", newTarget.ID, newTarget.State)
	} else if existingTarget.State != newTarget.State || existingTarget.URL != newTarget.URL {
		// Target updated
		kd.targets[newTarget.ID] = newTarget
		logutil.Printf("DEBUG", "Updated target: %s (state: %s -> %s)", newTarget.ID, existingTarget.State, newTarget.State)
	} else {
		// Just update last seen time
		existingTarget.LastSeen = newTarget.LastSeen
	}
}

// Helper methods (simplified versions of existing ScraperManager methods)

func (kd *KubernetesDiscovery) getMatchingNamespaces(namespaceSelector map[string]interface{}) ([]string, error) {
	if namespaceSelector == nil {
		return []string{"default"}, nil
	}

	// Handle matchNames
	if matchNames, ok := namespaceSelector["matchNames"].([]interface{}); ok {
		var namespaces []string
		for _, ns := range matchNames {
			if nsStr, ok := ns.(string); ok {
				namespaces = append(namespaces, nsStr)
			}
		}
		return namespaces, nil
	}

	// For now, return default namespace if no specific selector
	return []string{"default"}, nil
}

func (kd *KubernetesDiscovery) getMatchingPods(namespace string, selector map[string]interface{}) ([]*corev1.Pod, error) {
	if selector == nil {
		return nil, fmt.Errorf("no selector provided")
	}

	// Handle matchLabels
	if matchLabels, ok := selector["matchLabels"].(map[string]interface{}); ok {
		labelSelector := make(map[string]string)
		for k, v := range matchLabels {
			if vStr, ok := v.(string); ok {
				labelSelector[k] = vStr
			}
		}
		return kd.k8sClient.GetPodsByLabels(namespace, labelSelector)
	}

	return nil, fmt.Errorf("unsupported selector type")
}

func (kd *KubernetesDiscovery) determineScheme(endpointScheme, port string, tlsConfig map[string]interface{}) string {
	// Explicit scheme takes precedence
	if endpointScheme != "" {
		return endpointScheme
	}

	// Check if port name indicates HTTPS
	if strings.ToLower(port) == "https" {
		return "https"
	}

	// Check if TLS config is present
	if tlsConfig != nil && len(tlsConfig) > 0 {
		return "https"
	}

	return "http"
}

// Placeholder methods for ServiceMonitor and StaticEndpoints
func (kd *KubernetesDiscovery) discoverServiceTargets(config DiscoveryConfig) {
	logutil.Printf("DEBUG", "ServiceMonitor discovery not yet implemented for %s", config.TargetName)
}

func (kd *KubernetesDiscovery) discoverStaticTargets(config DiscoveryConfig) {
	logutil.Printf("DEBUG", "StaticEndpoints discovery not yet implemented for %s", config.TargetName)
}

// parseDiscoveryConfig parses target configuration into DiscoveryConfig
func (kd *KubernetesDiscovery) parseDiscoveryConfig(targetConfig map[string]interface{}) (DiscoveryConfig, error) {
	config := DiscoveryConfig{
		Enabled: true, // Default to enabled
	}

	// Parse basic fields
	if targetName, ok := targetConfig["targetName"].(string); ok {
		config.TargetName = targetName
	}

	if targetType, ok := targetConfig["type"].(string); ok {
		config.Type = targetType
	}

	if enabled, ok := targetConfig["enabled"].(bool); ok {
		config.Enabled = enabled
	}

	// Parse namespace selector
	if namespaceSelector, ok := targetConfig["namespaceSelector"].(map[string]interface{}); ok {
		config.NamespaceSelector = namespaceSelector
	}

	// Parse selector
	if selector, ok := targetConfig["selector"].(map[string]interface{}); ok {
		config.Selector = selector
	}

	// Parse endpoints
	if endpoints, ok := targetConfig["endpoints"].([]interface{}); ok {
		config.Endpoints = make([]EndpointConfig, 0, len(endpoints))
		for _, ep := range endpoints {
			if epMap, ok := ep.(map[string]interface{}); ok {
				endpointConfig := kd.parseEndpointConfig(epMap)
				config.Endpoints = append(config.Endpoints, endpointConfig)
			}
		}
	}

	// Parse other fields
	if scheme, ok := targetConfig["scheme"].(string); ok {
		config.Scheme = scheme
	}

	if path, ok := targetConfig["path"].(string); ok {
		config.Path = path
	}

	if interval, ok := targetConfig["interval"].(string); ok {
		config.Interval = interval
	}

	return config, nil
}

func (kd *KubernetesDiscovery) parseEndpointConfig(endpointMap map[string]interface{}) EndpointConfig {
	config := EndpointConfig{}

	if port, ok := endpointMap["port"].(string); ok {
		config.Port = port
	}

	if path, ok := endpointMap["path"].(string); ok {
		config.Path = path
	}

	if scheme, ok := endpointMap["scheme"].(string); ok {
		config.Scheme = scheme
	}

	if interval, ok := endpointMap["interval"].(string); ok {
		config.Interval = interval
	}

	if tlsConfig, ok := endpointMap["tlsConfig"].(map[string]interface{}); ok {
		config.TLSConfig = tlsConfig
	}

	if metricRelabelConfigs, ok := endpointMap["metricRelabelConfigs"].([]interface{}); ok {
		config.MetricRelabelConfigs = metricRelabelConfigs
	}

	if addNodeLabel, ok := endpointMap["addNodeLabel"].(bool); ok {
		config.AddNodeLabel = addNodeLabel
	}

	return config
}
