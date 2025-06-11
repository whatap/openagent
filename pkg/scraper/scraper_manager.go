package scraper

import (
	"fmt"
	"log"
	"strings"
	"time"

	"open-agent/pkg/client"
	"open-agent/pkg/config"
	"open-agent/pkg/k8s"
	"open-agent/pkg/model"
)

// ScraperManager is responsible for managing scraper tasks
//
// Scheme Determination Logic:
// 1. For PodMonitor and ServiceMonitor targets:
//   - If port name is "https", default to HTTPS
//   - Otherwise, default to HTTP
//
// 2. For StaticEndpoints targets:
//   - If TLS config is present, default to HTTPS
//   - Otherwise, default to HTTP
//
// 3. In all cases, explicit scheme configuration in the endpoint or target overrides the default
type ScraperManager struct {
	configManager *config.ConfigManager
	rawQueue      chan *model.ScrapeRawData
}

// matchNamespaceSelector checks if a namespace matches the namespace selector
func (sm *ScraperManager) matchNamespaceSelector(namespaceName string, namespaceLabels map[string]string, namespaceSelector map[string]interface{}) bool {
	// If no namespace selector is provided, don't match any namespaces
	if namespaceSelector == nil {
		return false
	}

	// Get the K8s client
	k8sClient := k8s.GetInstance()
	if !k8sClient.IsInitialized() {
		log.Printf("Kubernetes client not initialized, falling back to direct matching")
		return sm.matchNamespaceSelectorDirect(namespaceName, namespaceLabels, namespaceSelector)
	}

	// Check if matchNames is provided
	if matchNames, ok := namespaceSelector["matchNames"].([]interface{}); ok {
		// If matchNames is empty, don't match any namespaces
		if len(matchNames) == 0 {
			return false
		}

		// Convert matchNames to []string
		matchNamesStr := make([]string, 0, len(matchNames))
		for _, ns := range matchNames {
			if nsStr, ok := ns.(string); ok {
				matchNamesStr = append(matchNamesStr, nsStr)
			}
		}

		// Get namespaces with the specified names
		namespaces, err := k8sClient.GetNamespacesByNames(matchNamesStr)
		if err != nil {
			log.Printf("Error getting namespaces by names: %v", err)
			return false
		}

		// Check if the namespace is in the list
		found := false
		for _, ns := range namespaces {
			if ns.Name == namespaceName {
				found = true
				// If we found the namespace, we can use its labels for further checks
				namespaceLabels = ns.Labels
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check if matchLabels is provided
	if matchLabels, ok := namespaceSelector["matchLabels"].(map[string]interface{}); ok {
		// Convert matchLabels to map[string]string
		matchLabelsStr := make(map[string]string)
		for k, v := range matchLabels {
			if vStr, ok := v.(string); ok {
				matchLabelsStr[k] = vStr
			}
		}

		// Get namespaces with the specified labels
		namespaces, err := k8sClient.GetNamespacesByLabels(matchLabelsStr)
		if err != nil {
			log.Printf("Error getting namespaces by labels: %v", err)
			return false
		}

		// Check if the namespace is in the list
		found := false
		for _, ns := range namespaces {
			if ns.Name == namespaceName {
				found = true
				// If we found the namespace, we can use its labels for further checks
				namespaceLabels = ns.Labels
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check if matchExpressions is provided
	if matchExpressions, ok := namespaceSelector["matchExpressions"].([]interface{}); ok {
		for _, expr := range matchExpressions {
			exprMap, ok := expr.(map[string]interface{})
			if !ok {
				continue
			}

			key, hasKey := exprMap["key"].(string)
			operator, hasOperator := exprMap["operator"].(string)
			values, hasValues := exprMap["values"].([]interface{})

			if !hasKey || !hasOperator {
				continue
			}

			switch operator {
			case "In":
				// The label must exist and its value must be in the specified values
				value, exists := namespaceLabels[key]
				if !exists {
					return false
				}
				if hasValues {
					found := false
					for _, v := range values {
						if vStr, ok := v.(string); ok && value == vStr {
							found = true
							break
						}
					}
					if !found {
						return false
					}
				}
			case "NotIn":
				// If the label exists, its value must not be in the specified values
				value, exists := namespaceLabels[key]
				if exists && hasValues {
					for _, v := range values {
						if vStr, ok := v.(string); ok && value == vStr {
							return false
						}
					}
				}
			case "Exists":
				// The label must exist
				_, exists := namespaceLabels[key]
				if !exists {
					return false
				}
			case "DoesNotExist":
				// The label must not exist
				_, exists := namespaceLabels[key]
				if exists {
					return false
				}
			}
		}
	}

	// If we've passed all checks, return true
	return true
}

// matchNamespaceSelectorDirect is a fallback method that directly matches a namespace against a selector
// without using the Kubernetes API
func (sm *ScraperManager) matchNamespaceSelectorDirect(namespaceName string, namespaceLabels map[string]string, namespaceSelector map[string]interface{}) bool {
	// Check if matchNames is provided
	if matchNames, ok := namespaceSelector["matchNames"].([]interface{}); ok {
		// If matchNames is empty, don't match any namespaces
		if len(matchNames) == 0 {
			return false
		}

		// Check if the namespace is in the matchNames list
		found := false
		for _, ns := range matchNames {
			if nsStr, ok := ns.(string); ok && nsStr == namespaceName {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check if matchLabels is provided
	if matchLabels, ok := namespaceSelector["matchLabels"].(map[string]interface{}); ok {
		// Check if all matchLabels are present in the namespace labels
		if !sm.hasLabels(namespaceLabels, matchLabels) {
			return false
		}
	}

	// Check if matchExpressions is provided
	if matchExpressions, ok := namespaceSelector["matchExpressions"].([]interface{}); ok {
		for _, expr := range matchExpressions {
			exprMap, ok := expr.(map[string]interface{})
			if !ok {
				continue
			}

			key, hasKey := exprMap["key"].(string)
			operator, hasOperator := exprMap["operator"].(string)
			values, hasValues := exprMap["values"].([]interface{})

			if !hasKey || !hasOperator {
				continue
			}

			switch operator {
			case "In":
				// The label must exist and its value must be in the specified values
				value, exists := namespaceLabels[key]
				if !exists {
					return false
				}
				if hasValues {
					found := false
					for _, v := range values {
						if vStr, ok := v.(string); ok && value == vStr {
							found = true
							break
						}
					}
					if !found {
						return false
					}
				}
			case "NotIn":
				// If the label exists, its value must not be in the specified values
				value, exists := namespaceLabels[key]
				if exists && hasValues {
					for _, v := range values {
						if vStr, ok := v.(string); ok && value == vStr {
							return false
						}
					}
				}
			case "Exists":
				// The label must exist
				_, exists := namespaceLabels[key]
				if !exists {
					return false
				}
			case "DoesNotExist":
				// The label must not exist
				_, exists := namespaceLabels[key]
				if exists {
					return false
				}
			}
		}
	}

	// If we've passed all checks, return true
	return true
}

// hasLabels checks if a set of labels contains all required labels
func (sm *ScraperManager) hasLabels(labels map[string]string, matchLabels map[string]interface{}) bool {
	// If matchLabels is empty, return false
	if len(matchLabels) == 0 {
		return false
	}

	// Check if all matchLabels are present in the labels
	for key, v := range matchLabels {
		if val, ok := v.(string); ok {
			// If the label doesn't exist or the value doesn't match, return false
			if labelVal, exists := labels[key]; !exists || labelVal != val {
				return false
			}
		}
	}
	return true
}

// matchPodSelector checks if pod labels match the pod selector
func (sm *ScraperManager) matchPodSelector(podLabels map[string]string, podSelector map[string]interface{}) bool {
	// If no pod selector is provided, don't match any pods
	if podSelector == nil {
		return false
	}

	// Check if matchLabels is provided
	matchLabels, hasMatchLabels := podSelector["matchLabels"].(map[string]interface{})
	if hasMatchLabels {
		// Check if all matchLabels are present in the pod labels
		if !sm.hasLabels(podLabels, matchLabels) {
			return false
		}
	}

	// Check if matchExpressions is provided
	if matchExpressions, ok := podSelector["matchExpressions"].([]interface{}); ok {
		for _, expr := range matchExpressions {
			exprMap, ok := expr.(map[string]interface{})
			if !ok {
				continue
			}

			key, hasKey := exprMap["key"].(string)
			operator, hasOperator := exprMap["operator"].(string)
			values, hasValues := exprMap["values"].([]interface{})

			if !hasKey || !hasOperator {
				continue
			}

			switch operator {
			case "In":
				// The label must exist and its value must be in the specified values
				value, exists := podLabels[key]
				if !exists {
					return false
				}
				if hasValues {
					found := false
					for _, v := range values {
						if vStr, ok := v.(string); ok && value == vStr {
							found = true
							break
						}
					}
					if !found {
						return false
					}
				}
			case "NotIn":
				// If the label exists, its value must not be in the specified values
				value, exists := podLabels[key]
				if exists && hasValues {
					for _, v := range values {
						if vStr, ok := v.(string); ok && value == vStr {
							return false
						}
					}
				}
			case "Exists":
				// The label must exist
				_, exists := podLabels[key]
				if !exists {
					return false
				}
			case "DoesNotExist":
				// The label must not exist
				_, exists := podLabels[key]
				if exists {
					return false
				}
			}
		}
	}

	// If we've passed all checks, return true
	return true
}

// NewScraperManager creates a new ScraperManager instance
func NewScraperManager(configManager *config.ConfigManager, rawQueue chan *model.ScrapeRawData) *ScraperManager {
	sm := &ScraperManager{
		configManager: configManager,
		rawQueue:      rawQueue,
	}

	return sm
}

// ReloadConfig reloads the configuration and restarts scraping
func (sm *ScraperManager) ReloadConfig() {
	log.Println("Reloading scraper configuration...")
	sm.StartScraping()
}

// StartScraping starts the scraping process
func (sm *ScraperManager) StartScraping() {
	// Get the configuration
	cm := sm.configManager.GetConfig()
	if cm == nil {
		log.Println("No configuration loaded.")
		return
	}

	// Get the scrape interval
	scrapeIntervalStr := sm.configManager.GetScrapeInterval()
	scrapeIntervalSeconds, err := sm.configManager.ParseInterval(scrapeIntervalStr)
	if err != nil {
		log.Printf("Error parsing scrape interval: %v. Using default of 15 seconds.", err)
		scrapeIntervalSeconds = 15
	}

	// Get the scrape configs
	scrapeConfigs := sm.configManager.GetScrapeConfigs()
	if scrapeConfigs == nil {
		log.Println("No scrape_configs found in configuration.")
		return
	}

	// Schedule scraper tasks for each scrape config
	for _, scrapeConfig := range scrapeConfigs {
		// Check if this is a new format target with a type field
		if targetType, ok := scrapeConfig["type"].(string); ok {
			// Get the target name
			targetName, ok := scrapeConfig["targetName"].(string)
			if !ok {
				log.Println("Skipping target with no targetName.")
				continue
			}

			// Handle the target based on its type
			switch targetType {
			case "PodMonitor": // Support both new and old names
				sm.handlePodMonitorTarget(targetName, scrapeConfig, time.Duration(scrapeIntervalSeconds)*time.Second)
			case "ServiceMonitor": // Support both new and old names
				sm.handleServiceMonitorTarget(targetName, scrapeConfig, time.Duration(scrapeIntervalSeconds)*time.Second)
			case "StaticEndpoints":
				sm.handleStaticEndpointsTarget(targetName, scrapeConfig, time.Duration(scrapeIntervalSeconds)*time.Second)
			default:
				log.Printf("Unknown target type: %s for target: %s", targetType, targetName)
			}
		} else {
			log.Println("Skipping scrape config with no target type.")
			continue
		}
	}
}

// scheduleScraper schedules a scraper task to run at regular intervals
func (sm *ScraperManager) scheduleScraper(jobName, target string, metricRelabelConfigs model.RelabelConfigs, interval time.Duration, tlsConfig *client.TLSConfig) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Create a scraper task
	scraperTask := NewScraperTask(jobName, target, metricRelabelConfigs, tlsConfig)

	// Run the scraper task immediately
	sm.runScraperTask(scraperTask)

	// Run the scraper task at regular intervals
	for range ticker.C {
		sm.runScraperTask(scraperTask)
	}
}

// scheduleScraperWithFilterConfig schedules a scraper task to run at regular intervals using filterConfig (for backward compatibility)
func (sm *ScraperManager) scheduleScraperWithFilterConfig(jobName, target string, filterConfig map[string]interface{}, interval time.Duration, tlsConfig *client.TLSConfig) {
	// Extract metricRelabelConfigs from filterConfig
	var metricRelabelConfigs model.RelabelConfigs
	if filterConfig != nil {
		if relabelConfigs, ok := filterConfig["metricRelabelConfigs"].([]interface{}); ok {
			metricRelabelConfigs = model.ParseRelabelConfigs(relabelConfigs)
		}
	}

	// Call the new scheduleScraper function with the extracted metricRelabelConfigs
	sm.scheduleScraper(jobName, target, metricRelabelConfigs, interval, tlsConfig)
}

// runScraperTask runs a scraper task and adds the result to the raw queue
func (sm *ScraperManager) runScraperTask(scraperTask *ScraperTask) {
	rawData, err := scraperTask.Run()
	if err != nil {
		log.Printf("Error running scraper task: %v", err)
		return
	}

	// Add the raw data to the queue
	sm.rawQueue <- rawData
}

// scheduleScraperTask schedules a scraper task to run at regular intervals
func (sm *ScraperManager) scheduleScraperTask(scraperTask *ScraperTask, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Run the scraper task immediately
	sm.runScraperTask(scraperTask)

	// Run the scraper task at regular intervals
	for range ticker.C {
		sm.runScraperTask(scraperTask)
	}
}

// AddRawData adds raw data to the queue
func (sm *ScraperManager) AddRawData(data *model.ScrapeRawData) {
	sm.rawQueue <- data
}

// handlePodMonitorTarget handles a PodMonitor target
func (sm *ScraperManager) handlePodMonitorTarget(targetName string, targetConfig map[string]interface{}, defaultInterval time.Duration) {
	log.Printf("Processing PodMonitor target: %s", targetName)

	// Get the namespace selector
	namespaceSelector, nsOk := targetConfig["namespaceSelector"].(map[string]interface{})
	if !nsOk {
		log.Printf("No namespaceSelector found for PodMonitor target: %s", targetName)
		return
	}

	// Get the pod selector (called 'selector' in the new format)
	podSelector, podOk := targetConfig["selector"].(map[string]interface{})
	if !podOk {
		log.Printf("No selector found for PodMonitor target: %s", targetName)
		return
	}

	// Get the endpoints
	endpoints, ok := targetConfig["endpoints"].([]interface{})
	if !ok {
		log.Printf("No endpoints found for PodMonitor target: %s", targetName)
		return
	}

	// Get the K8s client
	k8sClient := k8s.GetInstance()
	if !k8sClient.IsInitialized() {
		log.Printf("Kubernetes client not initialized, using dummy target for PodMonitor: %s", targetName)
		// Fall back to dummy target
		sm.handlePodMonitorTargetWithDummyTarget(targetName, targetConfig, defaultInterval)
		return
	}

	// Get matching namespaces
	var namespaces []string
	if matchNames, ok := namespaceSelector["matchNames"].([]interface{}); ok {
		for _, ns := range matchNames {
			if nsStr, ok := ns.(string); ok {
				namespaces = append(namespaces, nsStr)
			}
		}
	}

	// Convert podSelector to map[string]string
	podLabels := make(map[string]string)
	if matchLabels, ok := podSelector["matchLabels"].(map[string]interface{}); ok {
		for key, v := range matchLabels {
			if val, ok := v.(string); ok {
				podLabels[key] = val
			}
		}
	}

	// Process each namespace
	for _, namespace := range namespaces {
		// Get pods matching the selector in this namespace
		pods, err := k8sClient.GetPodsByLabels(namespace, podLabels)
		if err != nil {
			log.Printf("Error getting pods in namespace %s for PodMonitor %s: %v", namespace, targetName, err)
			continue
		}

		if len(pods) == 0 {
			log.Printf("No pods found in namespace %s matching selector for PodMonitor %s", namespace, targetName)
			continue
		}

		// Process each endpoint
		for _, endpoint := range endpoints {
			endpointMap, ok := endpoint.(map[string]interface{})
			if !ok {
				continue
			}

			// Get the port
			portName, ok := endpointMap["port"].(string)
			if !ok {
				log.Printf("No port found in endpoint for PodMonitor target: %s", targetName)
				continue
			}

			// Get the path
			path, ok := endpointMap["path"].(string)
			if !ok {
				// Check if path is defined at the target level
				if targetPath, ok := targetConfig["path"].(string); ok {
					path = targetPath
				} else {
					log.Printf("No path found in endpoint for PodMonitor target: %s", targetName)
					continue
				}
			}

			// Get the interval
			endpointInterval := defaultInterval
			if intervalStr, ok := endpointMap["interval"].(string); ok {
				if intervalSeconds, err := sm.configManager.ParseInterval(intervalStr); err == nil {
					endpointInterval = time.Duration(intervalSeconds) * time.Second
				}
			} else if intervalStr, ok := targetConfig["interval"].(string); ok {
				// Check if interval is defined at the target level
				if intervalSeconds, err := sm.configManager.ParseInterval(intervalStr); err == nil {
					endpointInterval = time.Duration(intervalSeconds) * time.Second
				}
			}

			// Get the scheme
			scheme := "http"
			// Check if port name indicates HTTPS
			if strings.ToLower(portName) == "https" {
				scheme = "https"
			}
			// Override with explicit configuration if provided
			if schemeStr, ok := endpointMap["scheme"].(string); ok {
				scheme = schemeStr
			} else if schemeStr, ok := targetConfig["scheme"].(string); ok {
				scheme = schemeStr
			}

			// Create a metric selector config
			metricSelectorConfig := make(map[string]interface{})
			metricSelectorConfig["enabled"] = true

			// Add metricRelabelConfigs if present
			if metricRelabelConfigs, ok := endpointMap["metricRelabelConfigs"].([]interface{}); ok && len(metricRelabelConfigs) > 0 {
				metricSelectorConfig["metricRelabelConfigs"] = metricRelabelConfigs
			} else if metricRelabelConfigs, ok := targetConfig["metricRelabelConfigs"].([]interface{}); ok && len(metricRelabelConfigs) > 0 {
				// Check if metricRelabelConfigs is defined at the target level
				metricSelectorConfig["metricRelabelConfigs"] = metricRelabelConfigs
			}

			// Process each pod
			for _, pod := range pods {
				// Skip pods that are not running
				if pod.Status.Phase != "Running" {
					continue
				}

				// Get the pod's IP
				podIP := pod.Status.PodIP
				if podIP == "" {
					continue
				}

				// We don't need to get the port number here since it will be resolved dynamically
				// when the scraper task runs

				// Extract TLS configuration
				var tlsConfig *client.TLSConfig
				if tlsConfigMap, ok := endpointMap["tlsConfig"].(map[string]interface{}); ok {
					tlsConfig = &client.TLSConfig{}
					if insecureSkipVerify, ok := tlsConfigMap["insecureSkipVerify"].(bool); ok {
						tlsConfig.InsecureSkipVerify = insecureSkipVerify
					}
				}

				// Extract metricRelabelConfigs from metricSelectorConfig
				var metricRelabelConfigs model.RelabelConfigs
				if metricSelectorConfig != nil {
					if relabelConfigs, ok := metricSelectorConfig["metricRelabelConfigs"].([]interface{}); ok {
						metricRelabelConfigs = model.ParseRelabelConfigs(relabelConfigs)
					}
				}

				// Create a PodMonitor scraper task
				scraperTask := NewPodMonitorScraperTask(
					targetName,
					namespace,
					podLabels,
					portName,
					path,
					scheme,
					metricRelabelConfigs,
					tlsConfig,
				)

				// Schedule the scraper task
				go sm.scheduleScraperTask(scraperTask, endpointInterval)
			}
		}
	}
}

// handlePodMonitorTargetWithDummyTarget handles a PodMonitor target with a dummy target
// This is used when the Kubernetes client is not initialized
func (sm *ScraperManager) handlePodMonitorTargetWithDummyTarget(targetName string, targetConfig map[string]interface{}, defaultInterval time.Duration) {
	log.Printf("Using dummy target for PodMonitor: %s", targetName)

	// Get the endpoints
	endpoints, ok := targetConfig["endpoints"].([]interface{})
	if !ok {
		log.Printf("No endpoints found for PodMonitor target: %s", targetName)
		return
	}

	// Process each endpoint
	for _, endpoint := range endpoints {
		endpointMap, ok := endpoint.(map[string]interface{})
		if !ok {
			continue
		}

		// Get the port
		port, ok := endpointMap["port"].(string)
		if !ok {
			log.Printf("No port found in endpoint for PodMonitor target: %s", targetName)
			continue
		}

		// Get the path
		path, ok := endpointMap["path"].(string)
		if !ok {
			// Check if path is defined at the target level
			if targetPath, ok := targetConfig["path"].(string); ok {
				path = targetPath
			} else {
				log.Printf("No path found in endpoint for PodMonitor target: %s", targetName)
				continue
			}
		}

		// Get the interval
		endpointInterval := defaultInterval
		if intervalStr, ok := endpointMap["interval"].(string); ok {
			if intervalSeconds, err := sm.configManager.ParseInterval(intervalStr); err == nil {
				endpointInterval = time.Duration(intervalSeconds) * time.Second
			}
		} else if intervalStr, ok := targetConfig["interval"].(string); ok {
			// Check if interval is defined at the target level
			if intervalSeconds, err := sm.configManager.ParseInterval(intervalStr); err == nil {
				endpointInterval = time.Duration(intervalSeconds) * time.Second
			}
		}

		// Get the scheme
		scheme := "http"
		// Check if port name indicates HTTPS
		if strings.ToLower(port) == "https" {
			scheme = "https"
		}
		// Override with explicit configuration if provided
		if schemeStr, ok := endpointMap["scheme"].(string); ok {
			scheme = schemeStr
		} else if schemeStr, ok := targetConfig["scheme"].(string); ok {
			scheme = schemeStr
		}

		// Extract TLS configuration
		var tlsConfig *client.TLSConfig
		if tlsConfigMap, ok := endpointMap["tlsConfig"].(map[string]interface{}); ok {
			tlsConfig = &client.TLSConfig{}
			if insecureSkipVerify, ok := tlsConfigMap["insecureSkipVerify"].(bool); ok {
				tlsConfig.InsecureSkipVerify = insecureSkipVerify
			}
		}

		// Create a dummy target URL
		target := fmt.Sprintf("%s://localhost:%s%s", scheme, port, path)

		// Extract metricRelabelConfigs
		var metricRelabelConfigs model.RelabelConfigs
		if relabelConfigs, ok := endpointMap["metricRelabelConfigs"].([]interface{}); ok && len(relabelConfigs) > 0 {
			metricRelabelConfigs = model.ParseRelabelConfigs(relabelConfigs)
		} else if relabelConfigs, ok := targetConfig["metricRelabelConfigs"].([]interface{}); ok && len(relabelConfigs) > 0 {
			// Check if metricRelabelConfigs is defined at the target level
			metricRelabelConfigs = model.ParseRelabelConfigs(relabelConfigs)
		}

		// Create a scraper task for the target
		scraperTask := NewScraperTask(targetName, target, metricRelabelConfigs, tlsConfig)

		// Schedule the scraper task
		go sm.scheduleScraperTask(scraperTask, endpointInterval)
	}
}

// handleServiceMonitorTarget handles a ServiceMonitor target
func (sm *ScraperManager) handleServiceMonitorTarget(targetName string, targetConfig map[string]interface{}, defaultInterval time.Duration) {
	log.Printf("Processing ServiceMonitor target: %s", targetName)

	// Get the namespace selector
	namespaceSelector, nsOk := targetConfig["namespaceSelector"].(map[string]interface{})
	if !nsOk {
		log.Printf("No namespaceSelector found for ServiceMonitor target: %s", targetName)
		return
	}

	// Get the service selector (called 'selector' in the new format)
	serviceSelector, svcOk := targetConfig["selector"].(map[string]interface{})
	if !svcOk {
		log.Printf("No selector found for ServiceMonitor target: %s", targetName)
		return
	}

	// Get the endpoint configurations
	endpointConfigs, ok := targetConfig["endpoints"].([]interface{})
	if !ok {
		log.Printf("No endpoints found for ServiceMonitor target: %s", targetName)
		return
	}

	// Get the K8s client
	k8sClient := k8s.GetInstance()
	if !k8sClient.IsInitialized() {
		log.Printf("Kubernetes client not initialized, using dummy target for ServiceMonitor: %s", targetName)
		// Fall back to dummy target
		sm.handleServiceMonitorTargetWithDummyTarget(targetName, targetConfig, defaultInterval)
		return
	}

	// Get matching namespaces
	var namespaces []string
	if matchNames, ok := namespaceSelector["matchNames"].([]interface{}); ok {
		for _, ns := range matchNames {
			if nsStr, ok := ns.(string); ok {
				namespaces = append(namespaces, nsStr)
			}
		}
	}

	// Convert serviceSelector to map[string]string
	serviceLabels := make(map[string]string)
	if matchLabels, ok := serviceSelector["matchLabels"].(map[string]interface{}); ok {
		for key, v := range matchLabels {
			if val, ok := v.(string); ok {
				serviceLabels[key] = val
			}
		}
	}

	// Process each namespace
	for _, namespace := range namespaces {
		// Get services matching the selector in this namespace
		services, err := k8sClient.GetServicesByLabels(namespace, serviceLabels)
		if err != nil {
			log.Printf("Error getting services in namespace %s for ServiceMonitor %s: %v", namespace, targetName, err)
			continue
		}

		if len(services) == 0 {
			log.Printf("No services found in namespace %s matching selector for ServiceMonitor %s", namespace, targetName)
			continue
		}

		// Process each service
		for _, service := range services {
			// Get endpoints for this service
			k8sEndpoints, err := k8sClient.GetEndpointsForService(namespace, service.Name)
			if err != nil {
				log.Printf("Error getting endpoints for service %s in namespace %s: %v", service.Name, namespace, err)
				continue
			}

			if k8sEndpoints == nil || len(k8sEndpoints.Subsets) == 0 {
				log.Printf("No endpoints found for service %s in namespace %s", service.Name, namespace)
				continue
			}

			// Process each endpoint configuration from the ServiceMonitor
			for _, endpointConfig := range endpointConfigs {
				endpointMap, ok := endpointConfig.(map[string]interface{})
				if !ok {
					continue
				}

				// Get the port
				portName, ok := endpointMap["port"].(string)
				if !ok {
					log.Printf("No port found in endpoint for ServiceMonitor target: %s", targetName)
					continue
				}

				// Get the path
				path, ok := endpointMap["path"].(string)
				if !ok {
					// Check if path is defined at the target level
					if targetPath, ok := targetConfig["path"].(string); ok {
						path = targetPath
					} else {
						log.Printf("No path found in endpoint for ServiceMonitor target: %s", targetName)
						continue
					}
				}

				// Get the interval
				endpointInterval := defaultInterval
				if intervalStr, ok := endpointMap["interval"].(string); ok {
					if intervalSeconds, err := sm.configManager.ParseInterval(intervalStr); err == nil {
						endpointInterval = time.Duration(intervalSeconds) * time.Second
					}
				} else if intervalStr, ok := targetConfig["interval"].(string); ok {
					// Check if interval is defined at the target level
					if intervalSeconds, err := sm.configManager.ParseInterval(intervalStr); err == nil {
						endpointInterval = time.Duration(intervalSeconds) * time.Second
					}
				}

				// Get the scheme
				scheme := "http"
				// Check if port name indicates HTTPS
				if strings.ToLower(portName) == "https" {
					scheme = "https"
				}
				// Override with explicit configuration if provided
				if schemeStr, ok := endpointMap["scheme"].(string); ok {
					scheme = schemeStr
				} else if schemeStr, ok := targetConfig["scheme"].(string); ok {
					scheme = schemeStr
				}

				// Create a metric selector config
				metricSelectorConfig := make(map[string]interface{})
				metricSelectorConfig["enabled"] = true

				// Add metricRelabelConfigs if present
				if metricRelabelConfigs, ok := endpointMap["metricRelabelConfigs"].([]interface{}); ok && len(metricRelabelConfigs) > 0 {
					metricSelectorConfig["metricRelabelConfigs"] = metricRelabelConfigs
				} else if metricRelabelConfigs, ok := targetConfig["metricRelabelConfigs"].([]interface{}); ok && len(metricRelabelConfigs) > 0 {
					// Check if metricRelabelConfigs is defined at the target level
					metricSelectorConfig["metricRelabelConfigs"] = metricRelabelConfigs
				}

				// Extract TLS configuration
				var tlsConfig *client.TLSConfig
				if tlsConfigMap, ok := endpointMap["tlsConfig"].(map[string]interface{}); ok {
					tlsConfig = &client.TLSConfig{}
					if insecureSkipVerify, ok := tlsConfigMap["insecureSkipVerify"].(bool); ok {
						tlsConfig.InsecureSkipVerify = insecureSkipVerify
					}
				}

				// Extract metricRelabelConfigs from metricSelectorConfig
				var metricRelabelConfigs model.RelabelConfigs
				if metricSelectorConfig != nil {
					if relabelConfigs, ok := metricSelectorConfig["metricRelabelConfigs"].([]interface{}); ok {
						metricRelabelConfigs = model.ParseRelabelConfigs(relabelConfigs)
					}
				}

				// Create a ServiceMonitor scraper task
				scraperTask := NewServiceMonitorScraperTask(
					targetName,
					namespace,
					serviceLabels,
					portName,
					path,
					scheme,
					metricRelabelConfigs,
					tlsConfig,
				)

				// Schedule the scraper task
				go sm.scheduleScraperTask(scraperTask, endpointInterval)
			}
		}
	}
}

// handleServiceMonitorTargetWithDummyTarget handles a ServiceMonitor target with a dummy target
// This is used when the Kubernetes client is not initialized
func (sm *ScraperManager) handleServiceMonitorTargetWithDummyTarget(targetName string, targetConfig map[string]interface{}, defaultInterval time.Duration) {
	log.Printf("Using dummy target for ServiceMonitor: %s", targetName)

	// Get the endpoints
	endpoints, ok := targetConfig["endpoints"].([]interface{})
	if !ok {
		log.Printf("No endpoints found for ServiceMonitor target: %s", targetName)
		return
	}

	// Process each endpoint
	for _, endpoint := range endpoints {
		endpointMap, ok := endpoint.(map[string]interface{})
		if !ok {
			continue
		}

		// Get the port
		port, ok := endpointMap["port"].(string)
		if !ok {
			log.Printf("No port found in endpoint for ServiceMonitor target: %s", targetName)
			continue
		}

		// Get the path
		path, ok := endpointMap["path"].(string)
		if !ok {
			// Check if path is defined at the target level
			if targetPath, ok := targetConfig["path"].(string); ok {
				path = targetPath
			} else {
				log.Printf("No path found in endpoint for ServiceMonitor target: %s", targetName)
				continue
			}
		}

		// Get the interval
		endpointInterval := defaultInterval
		if intervalStr, ok := endpointMap["interval"].(string); ok {
			if intervalSeconds, err := sm.configManager.ParseInterval(intervalStr); err == nil {
				endpointInterval = time.Duration(intervalSeconds) * time.Second
			}
		} else if intervalStr, ok := targetConfig["interval"].(string); ok {
			// Check if interval is defined at the target level
			if intervalSeconds, err := sm.configManager.ParseInterval(intervalStr); err == nil {
				endpointInterval = time.Duration(intervalSeconds) * time.Second
			}
		}

		// Get the scheme
		scheme := "http"
		// Check if port name indicates HTTPS
		if strings.ToLower(port) == "https" {
			scheme = "https"
		}
		// Override with explicit configuration if provided
		if schemeStr, ok := endpointMap["scheme"].(string); ok {
			scheme = schemeStr
		} else if schemeStr, ok := targetConfig["scheme"].(string); ok {
			scheme = schemeStr
		}

		// Create a dummy target URL
		target := fmt.Sprintf("%s://localhost:%s%s", scheme, port, path)

		// Extract TLS configuration
		var tlsConfig *client.TLSConfig
		if tlsConfigMap, ok := endpointMap["tlsConfig"].(map[string]interface{}); ok {
			tlsConfig = &client.TLSConfig{}
			if insecureSkipVerify, ok := tlsConfigMap["insecureSkipVerify"].(bool); ok {
				tlsConfig.InsecureSkipVerify = insecureSkipVerify
			}
		}

		// Extract metricRelabelConfigs
		var metricRelabelConfigs model.RelabelConfigs
		if relabelConfigs, ok := endpointMap["metricRelabelConfigs"].([]interface{}); ok && len(relabelConfigs) > 0 {
			metricRelabelConfigs = model.ParseRelabelConfigs(relabelConfigs)
		} else if relabelConfigs, ok := targetConfig["metricRelabelConfigs"].([]interface{}); ok && len(relabelConfigs) > 0 {
			// Check if metricRelabelConfigs is defined at the target level
			metricRelabelConfigs = model.ParseRelabelConfigs(relabelConfigs)
		}

		// Create a scraper task for the target
		scraperTask := NewScraperTask(targetName, target, metricRelabelConfigs, tlsConfig)

		// Schedule the scraper task
		go sm.scheduleScraperTask(scraperTask, endpointInterval)
	}
}

// handleStaticEndpointsTarget handles a StaticEndpoints target
func (sm *ScraperManager) handleStaticEndpointsTarget(targetName string, targetConfig map[string]interface{}, defaultInterval time.Duration) {
	log.Printf("Processing StaticEndpoints target: %s", targetName)

	// Get the addresses
	addresses, ok := targetConfig["addresses"].([]interface{})
	if !ok {
		log.Printf("No addresses found for StaticEndpoints target: %s", targetName)
		return
	}

	// Get the path
	path, ok := targetConfig["path"].(string)
	if !ok {
		log.Printf("No path found for StaticEndpoints target: %s", targetName)
		return
	}

	// Get the interval
	interval := defaultInterval
	if intervalStr, ok := targetConfig["interval"].(string); ok {
		if intervalSeconds, err := sm.configManager.ParseInterval(intervalStr); err == nil {
			interval = time.Duration(intervalSeconds) * time.Second
		}
	}

	// Get the scheme
	scheme := "http"
	// For StaticEndpoints, we can't infer the scheme from a port name
	// If TLS config is present, default to HTTPS
	if _, ok := targetConfig["tlsConfig"].(map[string]interface{}); ok {
		scheme = "https"
	}
	// Override with explicit configuration if provided
	if schemeStr, ok := targetConfig["scheme"].(string); ok {
		scheme = schemeStr
	}

	// Get the labels
	labels := make(map[string]string)
	if labelsMap, ok := targetConfig["labels"].(map[string]interface{}); ok {
		for key, value := range labelsMap {
			if strValue, ok := value.(string); ok {
				labels[key] = strValue
			}
		}
	}

	// Extract TLS configuration
	var tlsConfig *client.TLSConfig
	if tlsConfigMap, ok := targetConfig["tlsConfig"].(map[string]interface{}); ok {
		tlsConfig = &client.TLSConfig{}
		if insecureSkipVerify, ok := tlsConfigMap["insecureSkipVerify"].(bool); ok {
			tlsConfig.InsecureSkipVerify = insecureSkipVerify
		}
	}

	// Create a metric selector config
	metricSelectorConfig := make(map[string]interface{})
	metricSelectorConfig["enabled"] = true

	// Add metricRelabelConfigs if present
	if metricRelabelConfigs, ok := targetConfig["metricRelabelConfigs"].([]interface{}); ok && len(metricRelabelConfigs) > 0 {
		metricSelectorConfig["metricRelabelConfigs"] = metricRelabelConfigs
	}

	// Process each address
	for _, address := range addresses {
		addressStr, ok := address.(string)
		if !ok {
			continue
		}

		// Extract metricRelabelConfigs from metricSelectorConfig
		var metricRelabelConfigs model.RelabelConfigs
		if metricSelectorConfig != nil {
			if relabelConfigs, ok := metricSelectorConfig["metricRelabelConfigs"].([]interface{}); ok {
				metricRelabelConfigs = model.ParseRelabelConfigs(relabelConfigs)
			}
		}

		// Create a StaticEndpoints scraper task
		scraperTask := NewStaticEndpointsScraperTask(
			targetName,
			addressStr,
			path,
			scheme,
			metricRelabelConfigs,
			tlsConfig,
		)

		// Schedule the scraper task
		go sm.scheduleScraperTask(scraperTask, interval)
	}
}
