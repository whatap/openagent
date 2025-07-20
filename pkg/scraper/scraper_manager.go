package scraper

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"time"

	"open-agent/pkg/client"
	"open-agent/pkg/config"
	"open-agent/pkg/discovery"
	"open-agent/pkg/k8s"
	"open-agent/pkg/model"
	"open-agent/tools/util/logutil"
)

// TargetScheduler manages individual target scraping with its own goroutine and ticker
type TargetScheduler struct {
	target   *discovery.Target
	interval time.Duration
	ticker   *time.Ticker
	stopCh   chan struct{}
	sm       *ScraperManager
	mutex    sync.RWMutex // target 접근 보호
}

// updateTarget safely updates the target reference
func (ts *TargetScheduler) updateTarget(newTarget *discovery.Target) {
	ts.mutex.Lock()
	defer ts.mutex.Unlock()
	ts.target = newTarget
}

// getTarget safely gets the current target
func (ts *TargetScheduler) getTarget() *discovery.Target {
	ts.mutex.RLock()
	defer ts.mutex.RUnlock()
	return ts.target
}

// ScraperManager is responsible for managing scraper tasks
// It now uses individual target schedulers instead of a global scheduler
type ScraperManager struct {
	configManager *config.ConfigManager
	discovery     discovery.ServiceDiscovery
	rawQueue      chan *model.ScrapeRawData

	// Individual target schedulers
	targetSchedulers map[string]*TargetScheduler
	schedulerMutex   sync.RWMutex

	// Track last scrape times to avoid over-scraping
	lastScrapeTime  map[string]time.Time
	lastScrapeMutex sync.RWMutex

	// Control channels
	stopCh chan struct{}
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
		logutil.Printf("INFO", "Kubernetes client not initialized, falling back to direct matching")
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
			logutil.Printf("ERROR", "Error getting namespaces by names: %v", err)
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
			logutil.Printf("ERROR", "Error getting namespaces by labels: %v", err)
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
func NewScraperManager(configManager *config.ConfigManager, discovery discovery.ServiceDiscovery, rawQueue chan *model.ScrapeRawData) *ScraperManager {
	sm := &ScraperManager{
		configManager:    configManager,
		discovery:        discovery,
		rawQueue:         rawQueue,
		targetSchedulers: make(map[string]*TargetScheduler),
		lastScrapeTime:   make(map[string]time.Time),
		stopCh:           make(chan struct{}),
	}

	return sm
}

// StartScraping starts the scraping process with individual target schedulers
func (sm *ScraperManager) StartScraping() {
	// Start target management loop
	go sm.targetManagementLoop()

	logutil.Println("INFO", "Individual target scraping started")
}

// targetManagementLoop manages individual target schedulers
func (sm *ScraperManager) targetManagementLoop() {
	// Get minimum interval for target management checks
	minimumIntervalStr := sm.configManager.GetMinimumInterval()
	minimumIntervalSeconds, err := sm.configManager.ParseInterval(minimumIntervalStr)
	if err != nil {
		logutil.Printf("WARN", "Error parsing minimum interval: %v. Using default of 1 second.", err)
		minimumIntervalSeconds = 1
	}

	// Use minimum interval for target management checks, but at least 5 seconds for efficiency
	managementInterval := time.Duration(minimumIntervalSeconds) * time.Second
	if managementInterval < 5*time.Second {
		managementInterval = 5 * time.Second
	}

	logutil.Printf("INFO", "[SCRAPER] Starting target management loop with interval: %v", managementInterval)
	ticker := time.NewTicker(managementInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			sm.updateTargetSchedulers()
		case <-sm.stopCh:
			sm.stopAllSchedulers()
			return
		}
	}
}

// calculateEndpointHash calculates SHA256 hash of endpoint configuration
func (sm *ScraperManager) calculateEndpointHash(endpoint interface{}) string {
	if endpoint == nil {
		return "null"
	}

	data, err := json.Marshal(endpoint)
	if err != nil {
		logutil.Printf("WARN", "Failed to marshal endpoint for hashing: %v", err)
		// Fallback: 강제로 변경된 것으로 처리
		return "error-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	}

	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// hasEndpointChanged checks if endpoint configuration has changed
func (sm *ScraperManager) hasEndpointChanged(oldTarget, newTarget *discovery.Target) bool {
	oldHash := sm.calculateEndpointHash(oldTarget.Metadata["endpoint"])
	newHash := sm.calculateEndpointHash(newTarget.Metadata["endpoint"])

	changed := oldHash != newHash

	if changed {
		logutil.Infof("hasEndpointChanged", "Endpoint changed for target %s: hash %s -> %s",
			newTarget.ID, oldHash[:8], newHash[:8])
	}

	return changed
}

// updateTargetSchedulers manages the lifecycle of individual target schedulers
func (sm *ScraperManager) updateTargetSchedulers() {
	// Get current ready targets
	targets := sm.discovery.GetReadyTargets()
	currentTargetIDs := make(map[string]bool)

	if config.IsDebugEnabled() {
		logutil.Printf("DEBUG", "Updating target schedulers for %d targets", len(targets))
	}

	// Start schedulers for new targets and update existing ones
	for _, target := range targets {
		currentTargetIDs[target.ID] = true

		sm.schedulerMutex.RLock()
		existingScheduler, exists := sm.targetSchedulers[target.ID]
		sm.schedulerMutex.RUnlock()

		if !exists {
			// Start new scheduler for this target
			sm.startTargetScheduler(target)
		} else {
			// Check if interval has changed (requires scheduler restart)
			newInterval := sm.getTargetInterval(target)
			if existingScheduler.interval != newInterval {
				logutil.Printf("INFO", "Target %s interval changed from %v to %v, restarting scheduler",
					target.ID, existingScheduler.interval, newInterval)
				sm.stopTargetScheduler(target.ID)
				sm.startTargetScheduler(target)
			} else if sm.hasEndpointChanged(existingScheduler.getTarget(), target) {
				// Endpoint changed but interval unchanged - graceful update without restart
				logutil.Printf("INFO", "Target %s endpoint configuration changed, applying from next scrape cycle", target.ID)
				existingScheduler.updateTarget(target)

				if config.IsDebugEnabled() {
					logutil.Printf("DEBUG", "Updated target reference for %s, new endpoint hash: %s",
						target.ID, sm.calculateEndpointHash(target.Metadata["endpoint"])[:8])
				}
			}
		}
	}

	// Stop schedulers for targets that are no longer ready
	sm.schedulerMutex.RLock()
	var schedulersToStop []string
	for targetID := range sm.targetSchedulers {
		if !currentTargetIDs[targetID] {
			schedulersToStop = append(schedulersToStop, targetID)
		}
	}
	sm.schedulerMutex.RUnlock()

	for _, targetID := range schedulersToStop {
		logutil.Printf("INFO", "Stopping scheduler for target %s (no longer ready)", targetID)
		sm.stopTargetScheduler(targetID)
	}
}

// startTargetScheduler starts an individual scheduler for a target
func (sm *ScraperManager) startTargetScheduler(target *discovery.Target) {
	interval := sm.getTargetInterval(target)

	// Apply minimum interval constraint
	minimumIntervalStr := sm.configManager.GetMinimumInterval()
	minimumIntervalSeconds, err := sm.configManager.ParseInterval(minimumIntervalStr)
	if err != nil {
		minimumIntervalSeconds = 1 // Default to 1 second
	}
	minimumInterval := time.Duration(minimumIntervalSeconds) * time.Second

	if interval < minimumInterval {
		logutil.Printf("WARN", "Target %s interval %v is less than minimum %v, using minimum",
			target.ID, interval, minimumInterval)
		interval = minimumInterval
	}

	scheduler := &TargetScheduler{
		target:   target,
		interval: interval,
		ticker:   time.NewTicker(interval),
		stopCh:   make(chan struct{}),
		sm:       sm,
	}

	sm.schedulerMutex.Lock()
	sm.targetSchedulers[target.ID] = scheduler
	sm.schedulerMutex.Unlock()

	// Start the scheduler goroutine
	go func() {
		defer scheduler.ticker.Stop()

		logutil.Printf("INFO", "[SCRAPER] Started scheduler for target %s with interval %v", target.ID, interval)

		for {
			select {
			case <-scheduler.ticker.C:
				// Use getTarget() to get the latest target information
				currentTarget := scheduler.getTarget()
				sm.scrapeTarget(currentTarget)
			case <-scheduler.stopCh:
				logutil.Printf("INFO", "[SCRAPER] Stopped scheduler for target %s", target.ID)
				return
			}
		}
	}()
}

// stopTargetScheduler stops an individual target scheduler
func (sm *ScraperManager) stopTargetScheduler(targetID string) {
	sm.schedulerMutex.Lock()
	defer sm.schedulerMutex.Unlock()

	if scheduler, exists := sm.targetSchedulers[targetID]; exists {
		close(scheduler.stopCh)
		delete(sm.targetSchedulers, targetID)
	}
}

// stopAllSchedulers stops all target schedulers
func (sm *ScraperManager) stopAllSchedulers() {
	sm.schedulerMutex.Lock()
	defer sm.schedulerMutex.Unlock()

	logutil.Printf("INFO", "Stopping all %d target schedulers", len(sm.targetSchedulers))

	for targetID, scheduler := range sm.targetSchedulers {
		close(scheduler.stopCh)
		if config.IsDebugEnabled() {
			logutil.Printf("DEBUG", "Stopped scheduler for target %s", targetID)
		}
	}

	// Clear the map
	sm.targetSchedulers = make(map[string]*TargetScheduler)
}

// Stop gracefully stops the scraper manager
func (sm *ScraperManager) Stop() {
	logutil.Printf("INFO", "Stopping ScraperManager")
	close(sm.stopCh)
}

// scrapeTarget performs scraping for a single target (called by individual schedulers)
func (sm *ScraperManager) scrapeTarget(target *discovery.Target) {
	// Add panic recovery to prevent individual target failures from crashing the scraper
	defer func() {
		if r := recover(); r != nil {
			logutil.Infoln("ERROR", "Panic recovered while scraping target %s: %v", target.ID, r)
		}
	}()

	// Log scraping interval information (debug only)
	if config.IsDebugEnabled() {
		sm.logScrapingInterval(target)
	}

	// Create scraper task from target
	scraperTask := sm.createScraperTaskFromTarget(target)
	if scraperTask == nil {
		logutil.Errorf("ERROR", "Failed to create scraper task for target: %s\n", target.ID)
		return
	}

	// Run the scraper task with error handling
	if err := sm.runScraperTaskWithError(scraperTask); err != nil {
		logutil.Errorf("ERROR", "Failed to scrape target %s: %v\n", target.ID, err)
		// Still update last scrape time for tracking
		sm.updateLastScrapingTime(target)
		return
	}

	// Update last scrape time on success
	sm.updateLastScrapingTime(target)
	if config.IsDebugEnabled() {
		logutil.Printf("DEBUG", "Successfully scraped target: %s", target.ID)
	}
}

// getTargetInterval gets the scraping interval for a target
func (sm *ScraperManager) getTargetInterval(target *discovery.Target) time.Duration {
	// Check for endpoint-specific interval
	if endpoint, ok := target.Metadata["endpoint"].(discovery.EndpointConfig); ok {
		if endpoint.Interval != "" {
			if intervalSeconds, err := sm.configManager.ParseInterval(endpoint.Interval); err == nil {
				return time.Duration(intervalSeconds) * time.Second
			} else {
				logutil.Infof("WARN", "Error parsing endpoint interval '%s' for target %s: %v. Using default of 60 seconds.\n",
					endpoint.Interval, target.ID, err)
			}
		}
	}

	// Default to 60 seconds if no endpoint interval is specified
	if config.IsDebugEnabled() {
		logutil.Infof("DEBUG", "No interval specified for target %s, using default of 60 seconds\n", target.ID)
	}
	return 60 * time.Second
}

// shouldSkipScraping checks if scraping should be skipped based on last scrape time
func (sm *ScraperManager) shouldSkipScraping(target *discovery.Target, interval time.Duration) bool {
	sm.lastScrapeMutex.RLock()
	lastScrape, exists := sm.lastScrapeTime[target.ID]
	sm.lastScrapeMutex.RUnlock()

	if !exists {
		return false // First time scraping this target
	}

	// Skip if not enough time has passed since last scrape
	return time.Since(lastScrape) < interval
}

// updateLastScrapingTime updates the last scraping time for a target
func (sm *ScraperManager) updateLastScrapingTime(target *discovery.Target) {
	sm.lastScrapeMutex.Lock()
	sm.lastScrapeTime[target.ID] = time.Now()
	sm.lastScrapeMutex.Unlock()
}

// logScrapingInterval logs the actual scraping interval for a target
func (sm *ScraperManager) logScrapingInterval(target *discovery.Target) {
	currentTime := time.Now()
	configuredInterval := sm.getTargetInterval(target)

	sm.lastScrapeMutex.RLock()
	lastScrape, exists := sm.lastScrapeTime[target.ID]
	sm.lastScrapeMutex.RUnlock()

	if exists {
		actualInterval := currentTime.Sub(lastScrape)

		// Log the interval information (debug only)
		if config.IsDebugEnabled() {
			logutil.Printf("DEBUG", "[SCRAPER] Target %s: interval deviation %v",
				target.ID, actualInterval-configuredInterval)
		}

		// Log warning if deviation is significant (more than 1 second)
		deviation := actualInterval - configuredInterval
		if deviation > time.Second || deviation < -time.Second {
			logutil.Printf("WARN", "[SCRAPER] Target %s has significant interval deviation: %v",
				target.ID, deviation)
		}
	}
}

// cleanupOldTargets removes old entries from lastScrapeTime to prevent memory leaks
func (sm *ScraperManager) cleanupOldTargets() {
	sm.lastScrapeMutex.Lock()
	defer sm.lastScrapeMutex.Unlock()

	// Get current ready targets to keep
	currentTargets := sm.discovery.GetReadyTargets()
	currentTargetIDs := make(map[string]bool)
	for _, target := range currentTargets {
		currentTargetIDs[target.ID] = true
	}

	// Remove entries that are not in current targets and are older than 1 hour
	cutoff := time.Now().Add(-1 * time.Hour)
	removedCount := 0

	for targetID, lastScrape := range sm.lastScrapeTime {
		// Remove if target is not current and last scrape was more than 1 hour ago
		if !currentTargetIDs[targetID] && lastScrape.Before(cutoff) {
			delete(sm.lastScrapeTime, targetID)
			removedCount++
		}
	}

	if removedCount > 0 && config.IsDebugEnabled() {
		logutil.Printf("DEBUG", "Cleaned up %d old target entries from memory", removedCount)
	}
}

// createScraperTaskFromTarget creates a ScraperTask from a discovery Target
func (sm *ScraperManager) createScraperTaskFromTarget(target *discovery.Target) *ScraperTask {
	// Extract metadata
	targetName, _ := target.Metadata["targetName"].(string)
	metricRelabelConfigs, _ := target.Metadata["metricRelabelConfigs"].([]interface{})

	// Debug log for target information (debug only)
	if config.IsDebugEnabled() {
		logutil.Printf("DEBUG", "[SCRAPER] Creating scraper task for target: %s", targetName)
	}

	// Parse metric relabel configs
	var relabelConfigs model.RelabelConfigs
	if metricRelabelConfigs != nil {
		relabelConfigs = model.ParseRelabelConfigs(metricRelabelConfigs)
		if config.IsDebugEnabled() {
			logutil.Printf("DEBUG", "[SCRAPER] Found %d metric relabel configs", len(metricRelabelConfigs))
		}
	}

	// Create TLS config if present
	var tlsConfig *client.TLSConfig
	if endpoint, ok := target.Metadata["endpoint"].(discovery.EndpointConfig); ok {
		if endpoint.TLSConfig != nil {
			tlsConfig = &client.TLSConfig{}
			if insecureSkipVerify, ok := endpoint.TLSConfig["insecureSkipVerify"].(bool); ok {
				tlsConfig.InsecureSkipVerify = insecureSkipVerify
				if config.IsDebugEnabled() {
					logutil.Printf("DEBUG", "[SCRAPER] TLS config: insecureSkipVerify=%v", insecureSkipVerify)
				}
			}
		}
	}

	// Extract node information for proper node label handling
	nodeName, _ := target.Labels["node"]
	addNodeLabel, _ := target.Metadata["addNodeLabel"].(bool)

	// Extract URL components
	path := extractPathFromURL(target.URL)
	scheme := extractSchemeFromURL(target.URL)

	// Create the scraper task using StaticEndpoints approach
	// ServiceDiscovery has already resolved the complete URL, so we use it directly
	scraperTask := NewStaticEndpointsScraperTask(
		targetName,
		target.URL, // Use the complete URL generated by ServiceDiscovery
		path,
		scheme,
		relabelConfigs,
		tlsConfig,
	)

	// Set node information for proper node label handling
	scraperTask.NodeName = nodeName
	scraperTask.AddNodeLabel = addNodeLabel

	// Debug log for created scraper task
	if config.IsDebugEnabled() {
		logutil.Printf("DEBUG", "[SCRAPER] Created scraper task: %s", scraperTask.TargetName)
	}

	return scraperTask
}

// Helper function to get map keys for debugging
func getMapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// Helper functions to extract URL components
func extractSchemeFromURL(url string) string {
	if strings.HasPrefix(url, "https://") {
		return "https"
	}
	return "http"
}

func extractPathFromURL(url string) string {
	// Simple path extraction - find the path after the port
	parts := strings.Split(url, "://")
	if len(parts) < 2 {
		return "/metrics"
	}

	hostPort := parts[1]
	pathIndex := strings.Index(hostPort, "/")
	if pathIndex == -1 {
		return "/metrics"
	}

	return hostPort[pathIndex:]
}

func (sm *ScraperManager) runScraperTaskWithError(scraperTask *ScraperTask) error {
	rawData, err := scraperTask.Run()
	if err != nil {
		logutil.Errorf("ERROR", "Error running scraper task: %v\n", err)
		return err
	}

	// Add the raw data to the queue
	sm.rawQueue <- rawData
	return nil
}

// AddRawData adds raw data to the queue
func (sm *ScraperManager) AddRawData(data *model.ScrapeRawData) {
	sm.rawQueue <- data
}

// Legacy methods - commented out as they're replaced by ServiceDiscovery
/*
// handlePodMonitorTarget handles a PodMonitor target
func (sm *ScraperManager) handlePodMonitorTarget(targetName string, targetConfig map[string]interface{}, defaultInterval time.Duration) {
	logutil.Printf("INFO", "Processing PodMonitor target: %s", targetName)

	// Get the namespace selector
	namespaceSelector, nsOk := targetConfig["namespaceSelector"].(map[string]interface{})
	if !nsOk {
		logutil.Printf("INFO", "No namespaceSelector found for PodMonitor target: %s", targetName)
		return
	}

	// Get the pod selector (called 'selector' in the new format)
	podSelector, podOk := targetConfig["selector"].(map[string]interface{})
	if !podOk {
		logutil.Printf("INFO", "No selector found for PodMonitor target: %s", targetName)
		return
	}

	// Get the endpoints
	endpoints, ok := targetConfig["endpoints"].([]interface{})
	if !ok {
		logutil.Printf("INFO", "No endpoints found for PodMonitor target: %s", targetName)
		return
	}

	// Get the K8s client
	k8sClient := k8s.GetInstance()
	if !k8sClient.IsInitialized() {
		logutil.Printf("INFO", "Kubernetes client not initialized, using dummy target for PodMonitor: %s", targetName)
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
			logutil.Printf("ERROR", "Error getting pods in namespace %s for PodMonitor %s: %v", namespace, targetName, err)
			continue
		}

		if len(pods) == 0 {
			logutil.Printf("INFO", "No pods found in namespace %s matching selector for PodMonitor %s", namespace, targetName)
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
				logutil.Printf("INFO", "No port found in endpoint for PodMonitor target: %s", targetName)
				continue
			}

			// Get the path
			path, ok := endpointMap["path"].(string)
			if !ok {
				// Check if path is defined at the target level
				if targetPath, ok := targetConfig["path"].(string); ok {
					path = targetPath
				} else {
					logutil.Printf("INFO", "No path found in endpoint for PodMonitor target: %s", targetName)
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

			// Get addNodeLabel configuration (default to false)
			addNodeLabel := false
			if addNodeLabelVal, ok := endpointMap["addNodeLabel"].(bool); ok {
				addNodeLabel = addNodeLabelVal
			} else if addNodeLabelVal, ok := targetConfig["addNodeLabel"].(bool); ok {
				addNodeLabel = addNodeLabelVal
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
					addNodeLabel,
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
	logutil.Printf("INFO", "Using dummy target for PodMonitor: %s", targetName)

	// Get the endpoints
	endpoints, ok := targetConfig["endpoints"].([]interface{})
	if !ok {
		logutil.Printf("INFO", "No endpoints found for PodMonitor target: %s", targetName)
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
			logutil.Printf("INFO", "No port found in endpoint for PodMonitor target: %s", targetName)
			continue
		}

		// Get the path
		path, ok := endpointMap["path"].(string)
		if !ok {
			// Check if path is defined at the target level
			if targetPath, ok := targetConfig["path"].(string); ok {
				path = targetPath
			} else {
				logutil.Printf("INFO", "No path found in endpoint for PodMonitor target: %s", targetName)
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
	logutil.Printf("INFO", "Processing ServiceMonitor target: %s", targetName)

	// Get the namespace selector
	namespaceSelector, nsOk := targetConfig["namespaceSelector"].(map[string]interface{})
	if !nsOk {
		logutil.Printf("INFO", "No namespaceSelector found for ServiceMonitor target: %s", targetName)
		return
	}

	// Get the service selector (called 'selector' in the new format)
	serviceSelector, svcOk := targetConfig["selector"].(map[string]interface{})
	if !svcOk {
		logutil.Printf("INFO", "No selector found for ServiceMonitor target: %s", targetName)
		return
	}

	// Get the endpoint configurations
	endpointConfigs, ok := targetConfig["endpoints"].([]interface{})
	if !ok {
		logutil.Printf("INFO", "No endpoints found for ServiceMonitor target: %s", targetName)
		return
	}

	// Get the K8s client
	k8sClient := k8s.GetInstance()
	if !k8sClient.IsInitialized() {
		logutil.Printf("INFO", "Kubernetes client not initialized, using dummy target for ServiceMonitor: %s", targetName)
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
			logutil.Printf("ERROR", "Error getting services in namespace %s for ServiceMonitor %s: %v", namespace, targetName, err)
			continue
		}

		if len(services) == 0 {
			logutil.Printf("INFO", "No services found in namespace %s matching selector for ServiceMonitor %s", namespace, targetName)
			continue
		}

		// Process each service
		for _, service := range services {
			// Get endpoints for this service
			k8sEndpoints, err := k8sClient.GetEndpointsForService(namespace, service.Name)
			if err != nil {
				logutil.Printf("ERROR", "Error getting endpoints for service %s in namespace %s: %v", service.Name, namespace, err)
				continue
			}

			if k8sEndpoints == nil || len(k8sEndpoints.Subsets) == 0 {
				logutil.Printf("INFO", "No endpoints found for service %s in namespace %s", service.Name, namespace)
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
					logutil.Printf("INFO", "No port found in endpoint for ServiceMonitor target: %s", targetName)
					continue
				}

				// Get the path
				path, ok := endpointMap["path"].(string)
				if !ok {
					// Check if path is defined at the target level
					if targetPath, ok := targetConfig["path"].(string); ok {
						path = targetPath
					} else {
						logutil.Printf("INFO", "No path found in endpoint for ServiceMonitor target: %s", targetName)
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
	logutil.Printf("INFO", "Using dummy target for ServiceMonitor: %s", targetName)

	// Get the endpoints
	endpoints, ok := targetConfig["endpoints"].([]interface{})
	if !ok {
		logutil.Printf("INFO", "No endpoints found for ServiceMonitor target: %s", targetName)
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
			logutil.Printf("INFO", "No port found in endpoint for ServiceMonitor target: %s", targetName)
			continue
		}

		// Get the path
		path, ok := endpointMap["path"].(string)
		if !ok {
			// Check if path is defined at the target level
			if targetPath, ok := targetConfig["path"].(string); ok {
				path = targetPath
			} else {
				logutil.Printf("INFO", "No path found in endpoint for ServiceMonitor target: %s", targetName)
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
	logutil.Printf("INFO", "Processing StaticEndpoints target: %s", targetName)

	// Get the addresses
	addresses, ok := targetConfig["addresses"].([]interface{})
	if !ok {
		logutil.Printf("INFO", "No addresses found for StaticEndpoints target: %s", targetName)
		return
	}

	// Get the path
	path, ok := targetConfig["path"].(string)
	if !ok {
		logutil.Printf("INFO", "No path found for StaticEndpoints target: %s", targetName)
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
*/
