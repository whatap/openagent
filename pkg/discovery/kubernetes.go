package discovery

import (
	"context"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	configPkg "open-agent/pkg/config"
	"open-agent/pkg/k8s"
	"open-agent/tools/util/logutil"
	"strings"
	"sync"
	"time"
)

// KubernetesDiscovery implements ServiceDiscovery for Kubernetes environments
type KubernetesDiscovery struct {
	configManager *configPkg.ConfigManager
	k8sClient     *k8s.K8sClient
	configs       []DiscoveryConfig
	targets       map[string]*Target
	targetsMutex  sync.RWMutex
	stopCh        chan struct{}
}

// NewKubernetesDiscovery creates a new KubernetesDiscovery instance
func NewKubernetesDiscovery(configManager *configPkg.ConfigManager) *KubernetesDiscovery {
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
		parseDiscoveryConfig, err := kd.parseDiscoveryConfig(targetConfig)
		if err != nil {
			logutil.Printf("ERROR", "Failed to parse target parseDiscoveryConfig: %v", err)
			continue
		}

		// Skip disabled targets
		if !parseDiscoveryConfig.Enabled {
			logutil.Printf("INFO", "Target %s is disabled, skipping", parseDiscoveryConfig.TargetName)
			continue
		}

		kd.configs = append(kd.configs, parseDiscoveryConfig)
	}

	logutil.Printf("INFO", "Loaded %d discovery configurations", len(kd.configs))
	return nil
}

// Start begins target discovery
func (kd *KubernetesDiscovery) Start(ctx context.Context) error {
	// Start periodic discovery
	go kd.discoveryLoop()

	logutil.Printf("INFO", "[DISCOVERY] Kubernetes service discovery started")
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

	// Debug logging for returned targets
	if configPkg.IsDebugEnabled() {
		logutil.Printf("DEBUG", "[DISCOVERY] Found %d ready targets out of %d total",
			len(readyTargets), len(kd.targets))
	}

	return readyTargets
}

// Stop stops the discovery process
func (kd *KubernetesDiscovery) Stop() error {
	close(kd.stopCh)
	logutil.Printf("INFO", "[DISCOVERY] Kubernetes service discovery stopped")
	return nil
}

// discoveryLoop runs the periodic target discovery
func (kd *KubernetesDiscovery) discoveryLoop() {
	// Initial discovery
	kd.discoverTargets()

	// Periodic discovery every 30 seconds (like Prometheus)
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			kd.discoverTargets()
		case <-kd.stopCh:
			return
		}
	}
}

// discoverTargets discovers all configured targets
func (kd *KubernetesDiscovery) discoverTargets() {
	// Get latest configuration from ConfigManager (uses Informer cache automatically)
	scrapeConfigs := kd.configManager.GetScrapeConfigs()
	if configPkg.IsDebugEnabled() {
		logutil.Printf("discoverTargets", "scrapeConfigs: %+v", scrapeConfigs)
	}
	if scrapeConfigs == nil {
		logutil.Printf("WARN", "No scrape configs available from ConfigManager")
		return
	}

	// Parse latest configurations into discovery configs
	currentConfigs := make([]DiscoveryConfig, 0)
	for _, targetConfig := range scrapeConfigs {
		parseDiscoveryConfig, err := kd.parseDiscoveryConfig(targetConfig)
		if err != nil {
			logutil.Printf("ERROR", "Failed to parse target config: %v", err)
			continue
		}

		// Skip disabled targets
		if !parseDiscoveryConfig.Enabled {
			if configPkg.IsDebugEnabled() {
				logutil.Printf("DEBUG", "Target %s is disabled, skipping", parseDiscoveryConfig.TargetName)
			}
			continue
		}

		currentConfigs = append(currentConfigs, parseDiscoveryConfig)
	}

	if configPkg.IsDebugEnabled() {
		logutil.Printf("DEBUG", "Using %d current discovery configurations from latest ConfigManager data", len(currentConfigs))
	}

	// Execute discovery with latest configurations
	for _, discoveryConfig := range currentConfigs {
		switch discoveryConfig.Type {
		case "PodMonitor":
			kd.discoverPodTargets(discoveryConfig)
		case "ServiceMonitor":
			kd.discoverServiceTargets(discoveryConfig)
		case "StaticEndpoints":
			kd.discoverStaticTargets(discoveryConfig)
		default:
			logutil.Printf("WARN", "Unknown target type: %s", discoveryConfig.Type)
		}
	}
}

// discoverPodTargets discovers Pod-based targets
func (kd *KubernetesDiscovery) discoverPodTargets(config DiscoveryConfig) {
	if configPkg.IsDebugEnabled() {
		logutil.Printf("DEBUG", "Discovering PodMonitor targets for %s", config.TargetName)
	}

	if !kd.k8sClient.IsInitialized() {
		logutil.Printf("WARN", "Kubernetes client not initialized for PodMonitor: %s", config.TargetName)
		return
	}

	// Debug: Log the configuration being used
	logutil.Printf("DEBUG", "PodMonitor %s - NamespaceSelector: %+v", config.TargetName, config.NamespaceSelector)
	logutil.Printf("DEBUG", "PodMonitor %s - Selector: %+v", config.TargetName, config.Selector)

	// Get matching namespaces
	namespaces, err := kd.getMatchingNamespaces(config.NamespaceSelector)
	if err != nil {
		logutil.Printf("ERROR", "Failed to get namespaces for %s: %v", config.TargetName, err)
		return
	}

	logutil.Printf("DEBUG", "PodMonitor %s - Found %d matching namespaces: %v", config.TargetName, len(namespaces), namespaces)

	totalPodsFound := 0
	for _, namespace := range namespaces {
		// Get matching pods
		pods, err := kd.getMatchingPods(namespace, config.Selector)
		if err != nil {
			logutil.Printf("ERROR", "Failed to get pods for %s in namespace %s: %v", config.TargetName, namespace, err)
			continue
		}

		logutil.Printf("DEBUG", "PodMonitor %s - Found %d pods in namespace %s", config.TargetName, len(pods), namespace)
		totalPodsFound += len(pods)

		for _, pod := range pods {
			logutil.Printf("DEBUG", "PodMonitor %s - Processing pod %s/%s with labels: %+v", config.TargetName, pod.Namespace, pod.Name, pod.Labels)
			kd.processPodTarget(pod, config)
		}
	}
	logutil.Printf("DEBUG", "PodMonitor %s - Total pods discovered: %d", config.TargetName, totalPodsFound)
}

// processPodTarget processes a single pod target
func (kd *KubernetesDiscovery) processPodTarget(pod *corev1.Pod, config DiscoveryConfig) {
	// Check if pod is ready
	isReady := kd.isPodReady(pod)

	for _, endpoint := range config.Endpoints {
		// Include path in targetID to ensure uniqueness when multiple endpoints use the same port
		pathSafe := strings.ReplaceAll(endpoint.Path, "/", "-")
		targetID := fmt.Sprintf("%s-%s-%s-%s-%s", config.TargetName, pod.Namespace, pod.Name, endpoint.Port, pathSafe)

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
		url := fmt.Sprintf("%s://%s:%s%s", scheme, podIP, endpoint.Port, path)

		// Create or update target
		target := &Target{
			ID:  targetID,
			URL: url,
			Labels: map[string]string{
				"job": config.TargetName,
			},
			Metadata: map[string]interface{}{
				"targetName":           config.TargetName,
				"type":                 config.Type,
				"endpoint":             endpoint,
				"metricRelabelConfigs": endpoint.MetricRelabelConfigs,
				"addNodeLabel":         endpoint.AddNodeLabel,
			},
			LastSeen: time.Now(),
		}
		// dear junnie
		// Add node label if requested
		// endpoint.AddNodeLabel means pod belongs to this Node, so we add 'node' label to metric&label cardinality
		// e.g) apiserver_request_total{status=200, instance=http://192.168.0.5:443}
		// clients can't find where instance is scheduled, so we put node like this, apiserver_request_total{status=200, instance=http://192.168.0.5:443, node=infra001}
		// therefore, this code is designed to processor can load nodeName and put into metric-label
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

	_, exists := kd.targets[newTarget.ID]

	if !exists {
		// New target
		kd.targets[newTarget.ID] = newTarget
		logutil.Printf("DEBUG", "Added new target: %s (state: %s)", newTarget.ID, newTarget.State)
	} else {
		// Always update target to ensure metadata changes are reflected
		// This includes metricRelabelConfigs changes from ConfigMap updates
		kd.targets[newTarget.ID] = newTarget
		logutil.Printf("DEBUG", "Updated target: %s (forced update to ensure metadata sync)", newTarget.ID)
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
			} else {
				logutil.Printf("WARN", "[DISCOVERY] Invalid namespace name type: %T", ns)
			}
		}
		if configPkg.IsDebugEnabled() {
			logutil.Printf("DEBUG", "[DISCOVERY] Found %d matching namespaces", len(namespaces))
		}
		return namespaces, nil
	}

	// For now, return default namespace if no specific selector
	return []string{"default"}, nil
}

func (kd *KubernetesDiscovery) getMatchingPods(namespace string, selector map[string]interface{}) ([]*corev1.Pod, error) {
	if selector == nil {
		logutil.Printf("ERROR", "[DISCOVERY] No selector provided for pod matching")
		return nil, fmt.Errorf("no selector provided")
	}

	// Handle matchLabels
	if matchLabels, ok := selector["matchLabels"].(map[string]interface{}); ok {
		labelSelector := make(map[string]string)
		for k, v := range matchLabels {
			if vStr, ok := v.(string); ok {
				labelSelector[k] = vStr
			} else {
				logutil.Printf("WARN", "[DISCOVERY] Invalid label value type for key %s: %T", k, v)
			}
		}
		if configPkg.IsDebugEnabled() {
			logutil.Printf("DEBUG", "[DISCOVERY] Matching pods in namespace %s with %d labels", namespace, len(labelSelector))
		}
		return kd.k8sClient.GetPodsByLabels(namespace, labelSelector)
	}

	logutil.Printf("ERROR", "[DISCOVERY] Unsupported selector type, expected matchLabels")
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

// ServiceMonitor and StaticEndpoints discovery implementations
func (kd *KubernetesDiscovery) discoverServiceTargets(config DiscoveryConfig) {
	logutil.Printf("DEBUG", "Discovering ServiceMonitor targets for %s", config.TargetName)

	if !kd.k8sClient.IsInitialized() {
		logutil.Printf("WARN", "Kubernetes client not initialized for ServiceMonitor: %s", config.TargetName)
		return
	}

	// Get matching namespaces
	namespaces, err := kd.getMatchingNamespaces(config.NamespaceSelector)
	if err != nil {
		logutil.Printf("ERROR", "Failed to get namespaces for %s: %v", config.TargetName, err)
		return
	}

	for _, namespace := range namespaces {
		// Get matching services
		services, err := kd.getMatchingServices(namespace, config.Selector)
		if err != nil {
			logutil.Printf("ERROR", "Failed to get services for %s in namespace %s: %v", config.TargetName, namespace, err)
			continue
		}

		for _, service := range services {
			kd.processServiceTarget(service, config)
		}
	}
}

// getMatchingServices gets services matching the selector in the given namespace
func (kd *KubernetesDiscovery) getMatchingServices(namespace string, selector map[string]interface{}) ([]*corev1.Service, error) {
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
		return kd.k8sClient.GetServicesByLabels(namespace, labelSelector)
	}

	return nil, fmt.Errorf("unsupported selector type")
}

// processServiceTarget processes a single service target
func (kd *KubernetesDiscovery) processServiceTarget(service *corev1.Service, config DiscoveryConfig) {
	// Get endpoints for this service
	endpoints, err := kd.k8sClient.GetEndpointsForService(service.Namespace, service.Name)
	if err != nil {
		logutil.Printf("ERROR", "Failed to get endpoints for service %s/%s: %v", service.Namespace, service.Name, err)
		return
	}

	// Process each configured endpoint
	for _, endpointConfig := range config.Endpoints {
		// Find the port in the service
		var targetPort string
		for _, servicePort := range service.Spec.Ports {
			if servicePort.Name == endpointConfig.Port || fmt.Sprintf("%d", servicePort.Port) == endpointConfig.Port {
				if servicePort.TargetPort.Type == 1 { // IntOrString type 1 = string
					targetPort = servicePort.TargetPort.StrVal
				} else {
					targetPort = fmt.Sprintf("%d", servicePort.TargetPort.IntVal)
				}
				break
			}
		}

		if targetPort == "" {
			logutil.Printf("WARN", "Port %s not found in service %s/%s", endpointConfig.Port, service.Namespace, service.Name)
			continue
		}

		// Process each endpoint address
		if endpoints != nil && len(endpoints.Subsets) > 0 {
			for subsetIdx, subset := range endpoints.Subsets {
				// Find the port in the subset
				var endpointPort int32
				for _, port := range subset.Ports {
					if port.Name == endpointConfig.Port || fmt.Sprintf("%d", port.Port) == endpointConfig.Port {
						endpointPort = port.Port
						break
					}
				}

				if endpointPort == 0 {
					logutil.Printf("DEBUG", "Port %s not found in endpoints for service %s/%s", endpointConfig.Port, service.Namespace, service.Name)
					continue
				}

				// Process ready addresses
				for addrIdx, address := range subset.Addresses {
					// Include path in targetID to ensure uniqueness when multiple endpoints use the same port
					pathSafe := strings.ReplaceAll(endpointConfig.Path, "/", "-")
					targetID := fmt.Sprintf("%s-%s-%s-%s-%d-%d-%s", config.TargetName, service.Namespace, service.Name, endpointConfig.Port, subsetIdx, addrIdx, pathSafe)

					// Determine scheme
					scheme := kd.determineScheme(endpointConfig.Scheme, endpointConfig.Port, endpointConfig.TLSConfig)

					// Build target URL
					path := endpointConfig.Path
					url := fmt.Sprintf("%s://%s:%d%s", scheme, address.IP, endpointPort, path)

					// Create target
					target := &Target{
						ID:  targetID,
						URL: url,
						Labels: map[string]string{
							"job":       config.TargetName,
							"namespace": service.Namespace,
							"service":   service.Name,
							"instance":  fmt.Sprintf("%s:%d", address.IP, endpointPort),
						},
						Metadata: map[string]interface{}{
							"targetName":           config.TargetName,
							"type":                 config.Type,
							"endpoint":             endpointConfig,
							"metricRelabelConfigs": endpointConfig.MetricRelabelConfigs,
						},
						State:    TargetStateReady, // Service endpoints are ready if they're in the addresses list
						LastSeen: time.Now(),
					}

					kd.updateTarget(target)
					logutil.Printf("DEBUG", "[DISCOVERY] Added ServiceMonitor target: %s", targetID)
				}

				// Process not-ready addresses as pending
				for addrIdx, address := range subset.NotReadyAddresses {
					// Include path in targetID to ensure uniqueness when multiple endpoints use the same port
					pathSafe := strings.ReplaceAll(endpointConfig.Path, "/", "-")
					targetID := fmt.Sprintf("%s-%s-%s-%s-%d-nr-%d-%s", config.TargetName, service.Namespace, service.Name, endpointConfig.Port, subsetIdx, addrIdx, pathSafe)

					// Determine scheme
					scheme := kd.determineScheme(endpointConfig.Scheme, endpointConfig.Port, endpointConfig.TLSConfig)

					// Build target URL
					path := endpointConfig.Path
					url := fmt.Sprintf("%s://%s:%d%s", scheme, address.IP, endpointPort, path)

					// Create target
					target := &Target{
						ID:  targetID,
						URL: url,
						Labels: map[string]string{
							"job": config.TargetName,
						},
						Metadata: map[string]interface{}{
							"targetName":           config.TargetName,
							"type":                 config.Type,
							"endpoint":             endpointConfig,
							"metricRelabelConfigs": endpointConfig.MetricRelabelConfigs,
						},
						State:    TargetStatePending, // Not ready endpoints are pending
						LastSeen: time.Now(),
					}

					kd.updateTarget(target)
					logutil.Printf("DEBUG", "[DISCOVERY] Added pending ServiceMonitor target: %s", targetID)
				}
			}
		} else {
			logutil.Printf("DEBUG", "No endpoints found for service %s/%s", service.Namespace, service.Name)
		}
	}
}

func (kd *KubernetesDiscovery) discoverStaticTargets(config DiscoveryConfig) {
	logutil.Printf("DEBUG", "Discovering StaticEndpoints targets for %s", config.TargetName)

	// StaticEndpoints don't require Kubernetes API - just process the configured endpoints
	if len(config.Endpoints) == 0 {
		logutil.Printf("WARN", "No endpoints configured for StaticEndpoints target: %s", config.TargetName)
		return
	}

	// Process each endpoint
	for i, endpoint := range config.Endpoints {
		if endpoint.Address == "" {
			logutil.Printf("WARN", "Empty address in endpoint %d for StaticEndpoints target: %s", i, config.TargetName)
			continue
		}

		// Determine scheme
		scheme := endpoint.Scheme
		if scheme == "" {
			// Default scheme based on TLS config
			if endpoint.TLSConfig != nil && len(endpoint.TLSConfig) > 0 {
				scheme = "https"
			} else {
				scheme = "http"
			}
		}

		// Determine path
		path := endpoint.Path
		if path == "" {
			path = "/metrics"
		}

		// Include path in targetID to ensure uniqueness when multiple endpoints use different paths
		pathSafe := strings.ReplaceAll(path, "/", "-")
		targetID := fmt.Sprintf("%s-static-%d-%s", config.TargetName, i, pathSafe)

		// Build target URL
		url := fmt.Sprintf("%s://%s%s", scheme, endpoint.Address, path)

		// Create target
		target := &Target{
			ID:  targetID,
			URL: url,
			Labels: map[string]string{
				"job":      config.TargetName,
				"instance": endpoint.Address,
			},
			Metadata: map[string]interface{}{
				"targetName":           config.TargetName,
				"type":                 config.Type,
				"endpoint":             endpoint,
				"metricRelabelConfigs": endpoint.MetricRelabelConfigs,
				"address":              endpoint.Address,
			},
			State:    TargetStateReady, // Static endpoints are always ready
			LastSeen: time.Now(),
		}

		kd.updateTarget(target)
		logutil.Printf("DEBUG", "Added StaticEndpoints target: %s (URL: %s)", targetID, url)
	}
}

// parseDiscoveryConfig parses target configuration into DiscoveryConfig
func (kd *KubernetesDiscovery) parseDiscoveryConfig(targetConfig map[string]interface{}) (DiscoveryConfig, error) {
	discoveryConfig := DiscoveryConfig{
		Enabled: true, // Default to enabled
	}

	// Parse basic fields
	if targetName, ok := targetConfig["targetName"].(string); ok {
		discoveryConfig.TargetName = targetName
	}

	if targetType, ok := targetConfig["type"].(string); ok {
		discoveryConfig.Type = targetType
	}

	if enabled, ok := targetConfig["enabled"].(bool); ok {
		discoveryConfig.Enabled = enabled
	}

	// Parse namespace selector
	if namespaceSelector, ok := targetConfig["namespaceSelector"].(map[string]interface{}); ok {
		discoveryConfig.NamespaceSelector = namespaceSelector
	}

	// Parse selector
	if selector, ok := targetConfig["selector"].(map[string]interface{}); ok {
		discoveryConfig.Selector = selector
	}

	// Parse endpoints
	if endpoints, ok := targetConfig["endpoints"].([]interface{}); ok {
		discoveryConfig.Endpoints = make([]EndpointConfig, 0, len(endpoints))
		for _, ep := range endpoints {
			if epMap, ok := ep.(map[string]interface{}); ok {
				endpointConfig := kd.parseEndpointConfig(epMap)
				discoveryConfig.Endpoints = append(discoveryConfig.Endpoints, endpointConfig)
			}
		}
	}

	return discoveryConfig, nil
}

func (kd *KubernetesDiscovery) parseEndpointConfig(endpointMap map[string]interface{}) EndpointConfig {
	endpointConfig := EndpointConfig{}

	if port, ok := endpointMap["port"].(string); ok {
		endpointConfig.Port = port
	}

	if address, ok := endpointMap["address"].(string); ok {
		endpointConfig.Address = address
	}

	if path, ok := endpointMap["path"].(string); ok {
		endpointConfig.Path = path
	}

	if scheme, ok := endpointMap["scheme"].(string); ok {
		endpointConfig.Scheme = scheme
	}

	if interval, ok := endpointMap["interval"].(string); ok {
		endpointConfig.Interval = interval
	}

	if tlsConfig, ok := endpointMap["tlsConfig"].(map[string]interface{}); ok {
		endpointConfig.TLSConfig = tlsConfig
	}

	if metricRelabelConfigs, ok := endpointMap["metricRelabelConfigs"].([]interface{}); ok {
		endpointConfig.MetricRelabelConfigs = metricRelabelConfigs
	}

	if addNodeLabel, ok := endpointMap["addNodeLabel"].(bool); ok {
		endpointConfig.AddNodeLabel = addNodeLabel
	}

	return endpointConfig
}
