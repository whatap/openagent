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
	logutil.Printf("DEBUG", "Discovering PodMonitor targets for %s", config.TargetName)

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
	logutil.Printf("DEBUG", "getMatchingNamespaces - namespaceSelector: %+v", namespaceSelector)

	if namespaceSelector == nil {
		logutil.Printf("DEBUG", "getMatchingNamespaces - No namespace selector provided, using default namespace")
		return []string{"default"}, nil
	}

	// Handle matchNames
	if matchNames, ok := namespaceSelector["matchNames"].([]interface{}); ok {
		logutil.Printf("DEBUG", "getMatchingNamespaces - Found matchNames: %+v", matchNames)
		var namespaces []string
		for _, ns := range matchNames {
			if nsStr, ok := ns.(string); ok {
				namespaces = append(namespaces, nsStr)
				logutil.Printf("DEBUG", "getMatchingNamespaces - Added namespace: %s", nsStr)
			} else {
				logutil.Printf("WARN", "getMatchingNamespaces - Invalid namespace name type: %T", ns)
			}
		}
		logutil.Printf("DEBUG", "getMatchingNamespaces - Final namespace list: %v", namespaces)
		return namespaces, nil
	}

	// For now, return default namespace if no specific selector
	logutil.Printf("DEBUG", "getMatchingNamespaces - No matchNames found, using default namespace")
	return []string{"default"}, nil
}

func (kd *KubernetesDiscovery) getMatchingPods(namespace string, selector map[string]interface{}) ([]*corev1.Pod, error) {
	logutil.Printf("DEBUG", "getMatchingPods - namespace: %s, selector: %+v", namespace, selector)

	if selector == nil {
		logutil.Printf("ERROR", "getMatchingPods - No selector provided")
		return nil, fmt.Errorf("no selector provided")
	}

	// Handle matchLabels
	if matchLabels, ok := selector["matchLabels"].(map[string]interface{}); ok {
		logutil.Printf("DEBUG", "getMatchingPods - Found matchLabels: %+v", matchLabels)
		labelSelector := make(map[string]string)
		for k, v := range matchLabels {
			if vStr, ok := v.(string); ok {
				labelSelector[k] = vStr
				logutil.Printf("DEBUG", "getMatchingPods - Added label selector: %s=%s", k, vStr)
			} else {
				logutil.Printf("WARN", "getMatchingPods - Invalid label value type for key %s: %T", k, v)
			}
		}
		logutil.Printf("DEBUG", "getMatchingPods - Final label selector: %+v", labelSelector)
		return kd.k8sClient.GetPodsByLabels(namespace, labelSelector)
	}

	logutil.Printf("ERROR", "getMatchingPods - Unsupported selector type, expected matchLabels")
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
					targetID := fmt.Sprintf("%s-%s-%s-%s-%d-%d", config.TargetName, service.Namespace, service.Name, endpointConfig.Port, subsetIdx, addrIdx)

					// Determine scheme
					scheme := kd.determineScheme(endpointConfig.Scheme, endpointConfig.Port, endpointConfig.TLSConfig)

					// Build target URL
					path := endpointConfig.Path
					if path == "" {
						path = config.Path
					}
					if path == "" {
						path = "/metrics"
					}

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
							"targetName":            config.TargetName,
							"type":                  config.Type,
							"endpoint":              endpointConfig,
							"metricRelabelConfigs":  endpointConfig.MetricRelabelConfigs,
							"service":               service,
						},
						State:    TargetStateReady, // Service endpoints are ready if they're in the addresses list
						LastSeen: time.Now(),
					}

					kd.updateTarget(target)
					logutil.Printf("DEBUG", "Added ServiceMonitor target: %s (URL: %s)", targetID, url)
				}

				// Process not-ready addresses as pending
				for addrIdx, address := range subset.NotReadyAddresses {
					targetID := fmt.Sprintf("%s-%s-%s-%s-%d-nr-%d", config.TargetName, service.Namespace, service.Name, endpointConfig.Port, subsetIdx, addrIdx)

					// Determine scheme
					scheme := kd.determineScheme(endpointConfig.Scheme, endpointConfig.Port, endpointConfig.TLSConfig)

					// Build target URL
					path := endpointConfig.Path
					if path == "" {
						path = config.Path
					}
					if path == "" {
						path = "/metrics"
					}

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
							"targetName":            config.TargetName,
							"type":                  config.Type,
							"endpoint":              endpointConfig,
							"metricRelabelConfigs":  endpointConfig.MetricRelabelConfigs,
							"service":               service,
						},
						State:    TargetStatePending, // Not ready endpoints are pending
						LastSeen: time.Now(),
					}

					kd.updateTarget(target)
					logutil.Printf("DEBUG", "Added pending ServiceMonitor target: %s (URL: %s)", targetID, url)
				}
			}
		} else {
			logutil.Printf("DEBUG", "No endpoints found for service %s/%s", service.Namespace, service.Name)
		}
	}
}

func (kd *KubernetesDiscovery) discoverStaticTargets(config DiscoveryConfig) {
	logutil.Printf("DEBUG", "Discovering StaticEndpoints targets for %s", config.TargetName)

	// StaticEndpoints don't require Kubernetes API - just process the configured addresses
	if len(config.Addresses) == 0 {
		logutil.Printf("WARN", "No addresses configured for StaticEndpoints target: %s", config.TargetName)
		return
	}

	// Determine scheme
	scheme := config.Scheme
	if scheme == "" {
		// Default scheme based on TLS config
		if config.TLSConfig != nil && len(config.TLSConfig) > 0 {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}

	// Determine path
	path := config.Path
	if path == "" {
		path = "/metrics"
	}

	// Process each address
	for i, address := range config.Addresses {
		targetID := fmt.Sprintf("%s-static-%d", config.TargetName, i)

		// Build target URL
		url := fmt.Sprintf("%s://%s%s", scheme, address, path)

		// Create target
		target := &Target{
			ID:  targetID,
			URL: url,
			Labels: map[string]string{
				"job":      config.TargetName,
				"instance": address,
			},
			Metadata: map[string]interface{}{
				"targetName":            config.TargetName,
				"type":                  config.Type,
				"address":               address,
				"metricRelabelConfigs":  config.MetricRelabelConfigs,
				"tlsConfig":             config.TLSConfig,
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

	// Parse addresses for StaticEndpoints
	if addresses, ok := targetConfig["addresses"].([]interface{}); ok {
		config.Addresses = make([]string, 0, len(addresses))
		for _, addr := range addresses {
			if addrStr, ok := addr.(string); ok {
				config.Addresses = append(config.Addresses, addrStr)
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

	// Parse TLS config at target level (for StaticEndpoints)
	if tlsConfig, ok := targetConfig["tlsConfig"].(map[string]interface{}); ok {
		config.TLSConfig = tlsConfig
	}

	// Parse metric relabel configs at target level (for StaticEndpoints)
	if metricRelabelConfigs, ok := targetConfig["metricRelabelConfigs"].([]interface{}); ok {
		config.MetricRelabelConfigs = metricRelabelConfigs
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
