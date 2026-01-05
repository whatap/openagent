package scraper

import (
	"fmt"
	corev1 "k8s.io/api/core/v1"
	"net/url"
	"strings"
	"time"

	"open-agent/pkg/client"
	"open-agent/pkg/config"
	"open-agent/pkg/k8s"
	"open-agent/pkg/model"
	"open-agent/tools/util/logutil"
)

// Use the package-level functions provided by the config package
// instead of creating our own instance of WhatapConfig

// TargetType represents the type of target to scrape
type TargetType string

const (
	// PodMonitorType represents a PodMonitor target
	PodMonitorType TargetType = "PodMonitor"
	// ServiceMonitorType represents a ServiceMonitor target
	ServiceMonitorType TargetType = "ServiceMonitor"
	// StaticEndpointsType represents a StaticEndpoints target
	StaticEndpointsType TargetType = "StaticEndpoints"
	// DirectURLType represents a direct URL target
	DirectURLType TargetType = "DirectURL"
)

// ScraperTask represents a task to scrape metrics from a target
type ScraperTask struct {
	TargetName           string
	TargetType           TargetType
	TargetURL            string            // Used for DirectURLType and as a fallback for other types
	Namespace            string            // Used for PodMonitorType and ServiceMonitorType
	Selector             map[string]string // Used for PodMonitorType and ServiceMonitorType
	Port                 string            // Used for PodMonitorType and ServiceMonitorType
	Path                 string            // Used for all types
	Scheme               string            // Used for all types
	Timeout              string            // HTTP timeout for the scrape request (e.g., "10s", "1m")
	MetricRelabelConfigs model.RelabelConfigs
	TLSConfig            *client.TLSConfig
	BasicAuth            *config.BasicAuthConfig
	Params               map[string][]string // HTTP URL parameters for the endpoint
	NodeName             string              // Used to store the node name for PodMonitor targets
	AddNodeLabel         bool                // Controls whether to add node label to metrics
}

// NewStaticEndpointsScraperTask creates a new ScraperTask instance for a StaticEndpoints target
func NewStaticEndpointsScraperTask(targetName string, targetURL string, path string, scheme string, metricRelabelConfigs model.RelabelConfigs, tlsConfig *client.TLSConfig) *ScraperTask {
	return &ScraperTask{
		TargetName:           targetName,
		TargetType:           StaticEndpointsType,
		TargetURL:            targetURL,
		Path:                 path,
		Scheme:               scheme,
		MetricRelabelConfigs: metricRelabelConfigs,
		TLSConfig:            tlsConfig,
	}
}

// appendParams appends query parameters to a URL if params are present
func (st *ScraperTask) appendParams(baseURL string) (string, error) {
	if len(st.Params) == 0 {
		return baseURL, nil
	}

	u, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse URL: %v", err)
	}

	query := u.Query()
	for key, values := range st.Params {
		for _, value := range values {
			query.Add(key, value)
		}
	}
	u.RawQuery = query.Encode()
	return u.String(), nil
}

// ResolveEndpoint resolves the endpoint URL based on the target information
func (st *ScraperTask) ResolveEndpoint() (string, error) {
	// If it's a direct URL, append params and return it
	if st.TargetType == DirectURLType {
		return st.appendParams(st.TargetURL)
	}

	// If it's a static endpoint, append params and return the target URL
	// ServiceDiscovery already provides a complete URL (e.g., http://10.21.130.48:9400/metrics)
	if st.TargetType == StaticEndpointsType {
		return st.appendParams(st.TargetURL)
	}

	// For PodMonitor and ServiceMonitor, we need to resolve the endpoint dynamically

	k8sClient := k8s.GetInstance()
	if !k8sClient.IsInitialized() {
		return "", fmt.Errorf("kubernetes client not initialized")
	}

	// For PodMonitor, get the pod IP and port
	if st.TargetType == PodMonitorType {
		// Get pods matching the selector in the namespace
		pods, err := k8sClient.GetPodsByLabels(st.Namespace, st.Selector)
		if err != nil {
			return "", fmt.Errorf("error getting pods in namespace %s: %v", st.Namespace, err)
		}

		if len(pods) == 0 {
			return "", fmt.Errorf("no pods found in namespace %s matching selector", st.Namespace)
		}

		// Use the first running pod
		var runningPod *corev1.Pod
		for _, p := range pods {
			if p.Status.Phase == "Running" {
				runningPod = p
				break
			}
		}

		if runningPod == nil {
			return "", fmt.Errorf("no running pods found in namespace %s matching selector", st.Namespace)
		}

		// Get the pod's IP
		podIP := runningPod.Status.PodIP
		if podIP == "" {
			return "", fmt.Errorf("pod %s has no IP", runningPod.Name)
		}

		// Get the node name
		st.NodeName = runningPod.Spec.NodeName

		// Get the port number
		port, err := k8sClient.GetPodPort(runningPod, st.Port)
		if err != nil {
			return "", fmt.Errorf("error getting port %s for pod %s: %v", st.Port, runningPod.Name, err)
		}

		// Create the target URL
		scheme := st.Scheme
		if scheme == "" {
			scheme = "http"
		}

		var baseURL string
		if st.Path != "" && !strings.HasPrefix(st.Path, "/") {
			baseURL = fmt.Sprintf("%s://%s:%d/%s", scheme, podIP, port, st.Path)
		} else {
			baseURL = fmt.Sprintf("%s://%s:%d%s", scheme, podIP, port, st.Path)
		}
		return st.appendParams(baseURL)
	}

	// For ServiceMonitor, get the service endpoints
	if st.TargetType == ServiceMonitorType {
		// Get services matching the selector in the namespace
		services, err := k8sClient.GetServicesByLabels(st.Namespace, st.Selector)
		if err != nil {
			return "", fmt.Errorf("error getting services in namespace %s: %v", st.Namespace, err)
		}

		if len(services) == 0 {
			return "", fmt.Errorf("no services found in namespace %s matching selector", st.Namespace)
		}

		// Use the first service
		service := services[0]

		// Get the endpoints for the service
		endpoints, err := k8sClient.GetEndpointsForService(st.Namespace, service.Name)
		if err != nil {
			return "", fmt.Errorf("error getting endpoints for service %s in namespace %s: %v", service.Name, st.Namespace, err)
		}

		if endpoints == nil || len(endpoints.Subsets) == 0 || len(endpoints.Subsets[0].Addresses) == 0 {
			return "", fmt.Errorf("no endpoints found for service %s in namespace %s", service.Name, st.Namespace)
		}

		// Use the first endpoint address
		endpointAddress := endpoints.Subsets[0].Addresses[0].IP

		// Get the target port number using GetServicePort
		port, err := k8sClient.GetServicePort(service, st.Port)
		if err != nil {
			return "", fmt.Errorf("error getting target port for service %s port %s: %v", service.Name, st.Port, err)
		}

		if port == 0 {
			return "", fmt.Errorf("port %s not found in service %s", st.Port, service.Name)
		}

		// Create the target URL
		scheme := st.Scheme
		if scheme == "" {
			scheme = "http"
		}

		var baseURL string
		if st.Path != "" && !strings.HasPrefix(st.Path, "/") {
			baseURL = fmt.Sprintf("%s://%s:%d/%s", scheme, endpointAddress, port, st.Path)
		} else {
			baseURL = fmt.Sprintf("%s://%s:%d%s", scheme, endpointAddress, port, st.Path)
		}
		return st.appendParams(baseURL)
	}

	return "", fmt.Errorf("unsupported target type: %s", st.TargetType)
}

// Run executes the scraper task
func (st *ScraperTask) Run() (*model.ScrapeRawData, error) {
	// Resolve the endpoint
	targetURL, resolveErr := st.ResolveEndpoint()
	if resolveErr != nil {
		logutil.Infof("SCRAPER", "Failed to resolve endpoint for target [%s]: %v", st.TargetName, resolveErr)
		if config.IsDebugEnabled() {
			logutil.Debugf("SCRAPER", "Error resolving endpoint for target %s: %v", st.TargetName, resolveErr)
		}
		return nil, fmt.Errorf("error resolving endpoint for target %s: %v", st.TargetName, resolveErr)
	}

	// Format the URL
	formattedURL := client.FormatURL(targetURL)

	// Log basic collection information at INFO level
	logutil.Infof("SCRAPER", "Starting collection from target [%s] at URL [%s]", st.TargetName, targetURL)

	// Log detailed information
	if config.IsDebugEnabled() {
		logutil.Debugf("SCRAPER", "Starting scraper task for target [%s], URL [%s]", st.TargetName, targetURL)
		logutil.Debugf("SCRAPER", "Formatted URL: %s", formattedURL)
		if st.TLSConfig != nil {
			logutil.Debugf("SCRAPER", "Using TLS config with InsecureSkipVerify=%v", st.TLSConfig.InsecureSkipVerify)
		}
		if len(st.MetricRelabelConfigs) > 0 {
			logutil.Debugf("SCRAPER", "Using %d metric relabel configs", len(st.MetricRelabelConfigs))
			for i, config := range st.MetricRelabelConfigs {
				logutil.Debugf("SCRAPER", "Relabel config #%d: Action=%s, SourceLabels=%v, TargetLabel=%s, Regex=%s",
					i+1, config.Action, config.SourceLabels, config.TargetLabel, config.Regex)
			}
		}
	}

	// Record start time for performance measurement
	startTime := time.Now()

	// Capture collection time right before making the HTTP request
	collectionTime := time.Now().UnixMilli()

	// Parse timeout if provided
	var timeout time.Duration
	if st.Timeout != "" {
		parsedTimeout, err := time.ParseDuration(st.Timeout)
		if err != nil {
			logutil.Infof("SCRAPER", "Invalid timeout format '%s' for target [%s], using default: %v", st.Timeout, st.TargetName, err)
			timeout = 0 // Will use default in HTTP client
		} else {
			timeout = parsedTimeout
			if config.IsDebugEnabled() {
				logutil.Debugf("SCRAPER", "Using timeout %v for target [%s]", timeout, st.TargetName)
			}
		}
	}

	// Execute the HTTP request
	httpClient := client.GetInstance()
	var response string
	var httpErr error

	response, httpErr = httpClient.ExecuteGetWithAuth(formattedURL, st.TLSConfig, st.BasicAuth, timeout)

	if httpErr != nil {
		logutil.Infof("SCRAPER", "Failed to collect from target [%s]: %v", st.TargetName, httpErr)
		if config.IsDebugEnabled() {
			logutil.Debugf("SCRAPER", "Error scraping target %s for target %s: %v", targetURL, st.TargetName, httpErr)
		}
		return nil, fmt.Errorf("error scraping target %s for target %s: %v", targetURL, st.TargetName, httpErr)
	}

	// Create a ScrapeRawData instance with the response
	var rawData *model.ScrapeRawData
	if st.NodeName != "" && st.AddNodeLabel {
		rawData = model.NewScrapeRawDataWithNodeName(targetURL, response, st.MetricRelabelConfigs, st.NodeName, st.AddNodeLabel, collectionTime)
	} else {
		rawData = model.NewScrapeRawData(targetURL, response, st.MetricRelabelConfigs, collectionTime)
	}

	// Log detailed information
	duration := time.Since(startTime)
	if config.IsDebugEnabled() {
		logutil.Debugf("SCRAPER", "Scraper task completed for target [%s], URL [%s] in %v", st.TargetName, targetURL, duration)
		logutil.Debugf("SCRAPER", "Response length: %d bytes", len(response))

		// Log a preview of the response (first 500 characters)
		preview := response
		if len(preview) > 500 {
			preview = preview[:500] + "..."
		}
		logutil.Debugf("SCRAPER", "Response preview: %s", preview)
	}

	// Count the number of metrics in the response (approximate)
	metricCount := 0
	for _, line := range strings.Split(response, "\n") {
		// Skip empty lines, comments, and metadata lines
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		metricCount++
	}

	// Log collection success with essential information at INFO level
	logutil.Infof("SCRAPER", "Successfully collected from target [%s]: %d metrics, %d bytes, took %v",
		st.TargetName, metricCount, len(response), duration)

	// Keep detailed debug information
	if config.IsDebugEnabled() {
		logutil.Debugf("SCRAPER", "Approximate number of metrics: %d", metricCount)
	}

	return rawData, nil
}
