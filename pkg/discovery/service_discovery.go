package discovery

import (
	"context"
	"fmt"
	"net/url"
	configPkg "open-agent/pkg/config"
	"open-agent/pkg/k8s"
	"open-agent/tools/util/logutil"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
)

// ServiceDiscoveryImpl implements service discovery for various target types including Kubernetes and static endpoints
type ServiceDiscoveryImpl struct {
	configManager *configPkg.ConfigManager
	k8sClient     *k8s.K8sClient
	configs       []DiscoveryConfig
	targets       map[string]*Target
	targetsMutex  sync.RWMutex
	stopCh        chan struct{}
}

// NewServiceDiscovery creates a new ServiceDiscoveryImpl instance
func NewServiceDiscovery(configManager *configPkg.ConfigManager) *ServiceDiscoveryImpl {
	return &ServiceDiscoveryImpl{
		configManager: configManager,
		k8sClient:     k8s.GetInstance(),
		targets:       make(map[string]*Target),
		stopCh:        make(chan struct{}),
	}
}

// buildURLWithParams constructs a URL with query parameters
func buildURLWithParams(baseURL string, params map[string]interface{}) string {
	if len(params) == 0 {
		return baseURL
	}

	u, err := url.Parse(baseURL)
	if err != nil {
		logutil.Printf("WARN", "Failed to parse URL %s: %v", baseURL, err)
		return baseURL
	}

	query := u.Query()
	for key, value := range params {
		switch v := value.(type) {
		case string:
			query.Set(key, v)
		case []interface{}:
			// Handle array values like in Prometheus config
			var strValues []string
			for _, item := range v {
				if strItem, ok := item.(string); ok {
					strValues = append(strValues, strItem)
				}
			}
			if len(strValues) > 0 {
				// For multiple values, join them with comma (Azure exporter style)
				query.Set(key, strings.Join(strValues, ","))
			}
		case []string:
			// Handle string array directly
			if len(v) > 0 {
				query.Set(key, strings.Join(v, ","))
			}
		default:
			// Convert other types to string
			query.Set(key, fmt.Sprintf("%v", v))
		}
	}

	u.RawQuery = query.Encode()
	return u.String()
}

func (sd *ServiceDiscoveryImpl) LoadTargets(targets []map[string]interface{}) error {
	sd.configs = make([]DiscoveryConfig, 0, len(targets))

	for _, targetConfig := range targets {
		parseDiscoveryConfig, err := sd.parseDiscoveryConfig(targetConfig)
		if err != nil {
			logutil.Infof("ERROR", "Failed to parse target parseDiscoveryConfig: %v", err)
			continue
		}

		// Skip disabled targets
		if !parseDiscoveryConfig.Enabled {
			logutil.Infof("INFO", "Target %s is disabled, skipping", parseDiscoveryConfig.TargetName)
			continue
		}

		sd.configs = append(sd.configs, parseDiscoveryConfig)
	}

	logutil.Infof("INFO", "Loaded %d discovery configurations", len(sd.configs))
	return nil
}

// Start begins target discovery
func (sd *ServiceDiscoveryImpl) Start(ctx context.Context) error {
	// Start periodic discovery
	go sd.discoveryLoop()

	logutil.Infof("INFO", "[DISCOVERY] Kubernetes service discovery started")
	return nil
}

// GetReadyTargets returns all targets in ready state
func (sd *ServiceDiscoveryImpl) GetReadyTargets() []*Target {
	sd.targetsMutex.RLock()
	defer sd.targetsMutex.RUnlock()

	var readyTargets []*Target
	for _, target := range sd.targets {
		if target.State == TargetStateReady {
			readyTargets = append(readyTargets, target)
		}
	}

	// Debug logging for returned targets
	if configPkg.IsDebugEnabled() {
		logutil.Debugf("DISCOVERY", "Found %d ready targets out of %d total",
			len(readyTargets), len(sd.targets))
	}

	return readyTargets
}

// Stop stops the discovery process
func (sd *ServiceDiscoveryImpl) Stop() error {
	close(sd.stopCh)
	logutil.Printf("INFO", "[DISCOVERY] Kubernetes service discovery stopped")
	return nil
}

// discoveryLoop runs the periodic target discovery
func (sd *ServiceDiscoveryImpl) discoveryLoop() {
	// Initial discovery
	sd.discoverTargets()

	// Periodic discovery every 30 seconds (like Prometheus)
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			sd.discoverTargets()
		case <-sd.stopCh:
			return
		}
	}
}

// discoverTargets discovers all configured targets
func (sd *ServiceDiscoveryImpl) discoverTargets() {
	// Get latest configuration from ConfigManager (uses Informer cache automatically)
	scrapeConfigs := sd.configManager.GetScrapeConfigs()
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
		parseDiscoveryConfig, err := sd.parseDiscoveryConfig(targetConfig)
		if err != nil {
			logutil.Printf("ERROR", "Failed to parse target config: %v", err)
			continue
		}

		// Skip disabled targets
		if !parseDiscoveryConfig.Enabled {
			if configPkg.IsDebugEnabled() {
				logutil.Debugf("DISCOVERY", "Target %s is disabled, skipping", parseDiscoveryConfig.TargetName)
			}
			continue
		}

		currentConfigs = append(currentConfigs, parseDiscoveryConfig)
	}

	if configPkg.IsDebugEnabled() {
		logutil.Debugf("DISCOVERY", "Using %d current discovery configurations from latest ConfigManager data", len(currentConfigs))
	}

	// Execute discovery with latest configurations
	for _, discoveryConfig := range currentConfigs {
		switch discoveryConfig.Type {
		case "PodMonitor":
			sd.discoverPodTargets(discoveryConfig)
		case "ServiceMonitor":
			sd.discoverServiceTargets(discoveryConfig)
		case "StaticEndpoints":
			sd.discoverStaticTargets(discoveryConfig)
		default:
			logutil.Infof("WARN", "Unknown target type: %s", discoveryConfig.Type)
		}
	}
}

// discoverPodTargets discovers Pod-based targets
func (sd *ServiceDiscoveryImpl) discoverPodTargets(config DiscoveryConfig) {
	if configPkg.IsDebugEnabled() {
		logutil.Debugf("DISCOVERY", "Discovering PodMonitor targets for %s", config.TargetName)
	}

	if !sd.k8sClient.IsInitialized() {
		logutil.Printf("WARN", "Kubernetes client not initialized for PodMonitor: %s", config.TargetName)
		return
	}

	// Debug: Log the configuration being used
	if configPkg.IsDebugEnabled() {
		logutil.Debugf("DISCOVERY", "PodMonitor %s - NamespaceSelector: %+v", config.TargetName, config.NamespaceSelector)
		logutil.Debugf("DISCOVERY", "PodMonitor %s - Selector: %+v", config.TargetName, config.Selector)
	}

	// Get matching namespaces
	namespaces, err := sd.getMatchingNamespaces(config.NamespaceSelector)
	if err != nil {
		logutil.Printf("ERROR", "Failed to get namespaces for %s: %v", config.TargetName, err)
		return
	}

	if configPkg.IsDebugEnabled() {
		logutil.Debugf("DISCOVERY", "PodMonitor %s - Found %d matching namespaces: %v", config.TargetName, len(namespaces), namespaces)
	}

	totalPodsFound := 0
	for _, namespace := range namespaces {
		// Get matching pods
		pods, err := sd.getMatchingPods(namespace, config.Selector)
		if err != nil {
			logutil.Printf("ERROR", "Failed to get pods for %s in namespace %s: %v", config.TargetName, namespace, err)
			continue
		}

		if configPkg.IsDebugEnabled() {
			logutil.Debugf("DISCOVERY", "PodMonitor %s - Found %d pods in namespace %s", config.TargetName, len(pods), namespace)
		}
		totalPodsFound += len(pods)

		for _, pod := range pods {
			if configPkg.IsDebugEnabled() {
				logutil.Debugf("DISCOVERY", "PodMonitor %s - Processing pod %s/%s with labels: %+v", config.TargetName, pod.Namespace, pod.Name, pod.Labels)
			}
			sd.processPodTarget(pod, config)
		}
	}
	if configPkg.IsDebugEnabled() {
		logutil.Debugf("DISCOVERY", "PodMonitor %s - Total pods discovered: %d", config.TargetName, totalPodsFound)
	}
}

func (sd *ServiceDiscoveryImpl) processPodTarget(pod *corev1.Pod, config DiscoveryConfig) {
	// Check if pod is ready
	isReady := sd.isPodReady(pod)

	for _, endpoint := range config.Endpoints {
		// Include path in targetID to ensure uniqueness when multiple endpoints use the same port
		pathSafe := strings.ReplaceAll(endpoint.Path, "/", "-")
		targetID := fmt.Sprintf("%s-%s-%s-%s-%s", config.TargetName, pod.Namespace, pod.Name, endpoint.Port, pathSafe)

		// Get pod IP
		podIP := pod.Status.PodIP
		if podIP == "" {
			if configPkg.IsDebugEnabled() {
				logutil.Debugf("DISCOVERY", "Pod %s/%s has no IP yet", pod.Namespace, pod.Name)
			}
			continue
		}

		// Determine scheme
		scheme := sd.determineScheme(endpoint.Scheme, endpoint.Port, endpoint.TLSConfig)

		// Build target URL
		path := endpoint.Path
		baseURL := fmt.Sprintf("%s://%s:%s%s", scheme, podIP, endpoint.Port, path)
		url := buildURLWithParams(baseURL, endpoint.Params)

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
			if configPkg.IsDebugEnabled() {
				logutil.Debugf("DISCOVERY", "Pod %s/%s is not ready yet", pod.Namespace, pod.Name)
			}
		}

		sd.updateTarget(target)
	}
}

// isPodReady checks if a pod is ready (same logic as in ScraperManager)
func (sd *ServiceDiscoveryImpl) isPodReady(pod *corev1.Pod) bool {
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
func (sd *ServiceDiscoveryImpl) updateTarget(newTarget *Target) {
	sd.targetsMutex.Lock()
	defer sd.targetsMutex.Unlock()

	_, exists := sd.targets[newTarget.ID]

	if !exists {
		// New target
		sd.targets[newTarget.ID] = newTarget
		if configPkg.IsDebugEnabled() {
			logutil.Debugf("DISCOVERY", "Added new target: %s (state: %s)", newTarget.ID, newTarget.State)
		}
	} else {
		// Always update target to ensure metadata changes are reflected
		// This includes metricRelabelConfigs changes from ConfigMap updates
		sd.targets[newTarget.ID] = newTarget
		if configPkg.IsDebugEnabled() {
			logutil.Debugf("DISCOVERY", "Updated target: %s (forced update to ensure metadata sync)", newTarget.ID)
		}
	}
}

// Helper methods (simplified versions of existing ScraperManager methods)

func (sd *ServiceDiscoveryImpl) getMatchingNamespaces(namespaceSelector map[string]interface{}) ([]string, error) {
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
			logutil.Debugf("DISCOVERY", "Found %d matching namespaces", len(namespaces))
		}
		return namespaces, nil
	}

	// For now, return default namespace if no specific selector
	return []string{"default"}, nil
}

func (sd *ServiceDiscoveryImpl) getMatchingPods(namespace string, selector map[string]interface{}) ([]*corev1.Pod, error) {
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
			logutil.Debugf("DISCOVERY", "Matching pods in namespace %s with %d labels", namespace, len(labelSelector))
		}
		return sd.k8sClient.GetPodsByLabels(namespace, labelSelector)
	}

	logutil.Printf("ERROR", "[DISCOVERY] Unsupported selector type, expected matchLabels")
	return nil, fmt.Errorf("unsupported selector type")
}

func (sd *ServiceDiscoveryImpl) determineScheme(endpointScheme, port string, tlsConfig map[string]interface{}) string {
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
func (sd *ServiceDiscoveryImpl) discoverServiceTargets(config DiscoveryConfig) {
	if configPkg.IsDebugEnabled() {
		logutil.Debugf("DISCOVERY", "Discovering ServiceMonitor targets for %s", config.TargetName)
	}

	if !sd.k8sClient.IsInitialized() {
		logutil.Printf("WARN", "Kubernetes client not initialized for ServiceMonitor: %s", config.TargetName)
		return
	}

	// Get matching namespaces
	namespaces, err := sd.getMatchingNamespaces(config.NamespaceSelector)
	if err != nil {
		logutil.Printf("ERROR", "Failed to get namespaces for %s: %v", config.TargetName, err)
		return
	}

	for _, namespace := range namespaces {
		// Get matching services
		services, err := sd.getMatchingServices(namespace, config.Selector)
		if err != nil {
			logutil.Printf("ERROR", "Failed to get services for %s in namespace %s: %v", config.TargetName, namespace, err)
			continue
		}

		for _, service := range services {
			sd.processServiceTarget(service, config)
		}
	}
}

// getMatchingServices gets services matching the selector in the given namespace
func (sd *ServiceDiscoveryImpl) getMatchingServices(namespace string, selector map[string]interface{}) ([]*corev1.Service, error) {
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
		return sd.k8sClient.GetServicesByLabels(namespace, labelSelector)
	}

	return nil, fmt.Errorf("unsupported selector type")
}

// processServiceTarget processes a single service target
func (sd *ServiceDiscoveryImpl) processServiceTarget(service *corev1.Service, config DiscoveryConfig) {
	// Get endpoints for this service
	endpoints, err := sd.k8sClient.GetEndpointsForService(service.Namespace, service.Name)
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
					if configPkg.IsDebugEnabled() {
						logutil.Debugf("DISCOVERY", "Port %s not found in endpoints for service %s/%s", endpointConfig.Port, service.Namespace, service.Name)
					}
					continue
				}

				// Process ready addresses
				for addrIdx, address := range subset.Addresses {
					// Include path in targetID to ensure uniqueness when multiple endpoints use the same port
					pathSafe := strings.ReplaceAll(endpointConfig.Path, "/", "-")
					targetID := fmt.Sprintf("%s-%s-%s-%s-%d-%d-%s", config.TargetName, service.Namespace, service.Name, endpointConfig.Port, subsetIdx, addrIdx, pathSafe)

					// Determine scheme
					scheme := sd.determineScheme(endpointConfig.Scheme, endpointConfig.Port, endpointConfig.TLSConfig)

					// Build target URL
					path := endpointConfig.Path
					baseURL := fmt.Sprintf("%s://%s:%d%s", scheme, address.IP, endpointPort, path)
					url := buildURLWithParams(baseURL, endpointConfig.Params)

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

					sd.updateTarget(target)
					if configPkg.IsDebugEnabled() {
						logutil.Debugf("DISCOVERY", "Added ServiceMonitor target: %s", targetID)
					}
				}

				// Process not-ready addresses as pending
				for addrIdx, address := range subset.NotReadyAddresses {
					// Include path in targetID to ensure uniqueness when multiple endpoints use the same port
					pathSafe := strings.ReplaceAll(endpointConfig.Path, "/", "-")
					targetID := fmt.Sprintf("%s-%s-%s-%s-%d-nr-%d-%s", config.TargetName, service.Namespace, service.Name, endpointConfig.Port, subsetIdx, addrIdx, pathSafe)

					// Determine scheme
					scheme := sd.determineScheme(endpointConfig.Scheme, endpointConfig.Port, endpointConfig.TLSConfig)

					// Build target URL
					path := endpointConfig.Path
					baseURL := fmt.Sprintf("%s://%s:%d%s", scheme, address.IP, endpointPort, path)
					url := buildURLWithParams(baseURL, endpointConfig.Params)

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

					sd.updateTarget(target)
					if configPkg.IsDebugEnabled() {
						logutil.Debugf("DISCOVERY", "Added pending ServiceMonitor target: %s", targetID)
					}
				}
			}
		} else {
			if configPkg.IsDebugEnabled() {
				logutil.Debugf("DISCOVERY", "No endpoints found for service %s/%s", service.Namespace, service.Name)
			}
		}
	}
}

func (sd *ServiceDiscoveryImpl) discoverStaticTargets(config DiscoveryConfig) {
	if configPkg.IsDebugEnabled() {
		logutil.Debugf("DISCOVERY", "Discovering StaticEndpoints targets for %s", config.TargetName)
	}

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
		baseURL := fmt.Sprintf("%s://%s%s", scheme, endpoint.Address, path)
		url := buildURLWithParams(baseURL, endpoint.Params)

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

		sd.updateTarget(target)
		if configPkg.IsDebugEnabled() {
			logutil.Debugf("DISCOVERY", "Added StaticEndpoints target: %s (URL: %s)", targetID, url)
		}
	}
}

// parseDiscoveryConfig parses target configuration into DiscoveryConfig
func (sd *ServiceDiscoveryImpl) parseDiscoveryConfig(targetConfig map[string]interface{}) (DiscoveryConfig, error) {
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
				endpointConfig := sd.parseEndpointConfig(epMap)
				discoveryConfig.Endpoints = append(discoveryConfig.Endpoints, endpointConfig)
			}
		}
	}

	return discoveryConfig, nil
}

func (sd *ServiceDiscoveryImpl) parseEndpointConfig(endpointMap map[string]interface{}) EndpointConfig {
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

	if addWeightedLabel, ok := endpointMap["addWeightedLabel"].(bool); ok {
		endpointConfig.AddWeightedLabel = addWeightedLabel
	}

	// Parse params for HTTP URL parameters
	if params, ok := endpointMap["params"].(map[string]interface{}); ok {
		endpointConfig.Params = params
	}

	return endpointConfig
}
