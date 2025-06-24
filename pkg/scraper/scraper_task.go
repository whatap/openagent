package scraper

import (
	"fmt"
	corev1 "k8s.io/api/core/v1"
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
	TargetURL            string // Used for DirectURLType and as a fallback for other types
	Namespace            string // Used for PodMonitorType and ServiceMonitorType
	Selector             map[string]string // Used for PodMonitorType and ServiceMonitorType
	Port                 string // Used for PodMonitorType and ServiceMonitorType
	Path                 string // Used for all types
	Scheme               string // Used for all types
	MetricRelabelConfigs model.RelabelConfigs
	TLSConfig            *client.TLSConfig
	NodeName             string // Used to store the node name for PodMonitor targets
	AddNodeLabel         bool   // Controls whether to add node label to metrics
}

// NewScraperTask creates a new ScraperTask instance
func NewScraperTask(targetName, targetURL string, metricRelabelConfigs model.RelabelConfigs, tlsConfig *client.TLSConfig) *ScraperTask {
	// For backward compatibility, create a DirectURL type task
	return &ScraperTask{
		TargetName:           targetName,
		TargetType:           DirectURLType,
		TargetURL:            targetURL,
		MetricRelabelConfigs: metricRelabelConfigs,
		TLSConfig:            tlsConfig,
	}
}

// NewPodMonitorScraperTask creates a new ScraperTask instance for a PodMonitor target
func NewPodMonitorScraperTask(targetName string, namespace string, selector map[string]string, port string, path string, scheme string, metricRelabelConfigs model.RelabelConfigs, tlsConfig *client.TLSConfig, addNodeLabel bool) *ScraperTask {
	return &ScraperTask{
		TargetName:           targetName,
		TargetType:           PodMonitorType,
		Namespace:            namespace,
		Selector:             selector,
		Port:                 port,
		Path:                 path,
		Scheme:               scheme,
		MetricRelabelConfigs: metricRelabelConfigs,
		TLSConfig:            tlsConfig,
		AddNodeLabel:         addNodeLabel,
	}
}

// NewServiceMonitorScraperTask creates a new ScraperTask instance for a ServiceMonitor target
func NewServiceMonitorScraperTask(targetName string, namespace string, selector map[string]string, port string, path string, scheme string, metricRelabelConfigs model.RelabelConfigs, tlsConfig *client.TLSConfig) *ScraperTask {
	return &ScraperTask{
		TargetName:           targetName,
		TargetType:           ServiceMonitorType,
		Namespace:            namespace,
		Selector:             selector,
		Port:                 port,
		Path:                 path,
		Scheme:               scheme,
		MetricRelabelConfigs: metricRelabelConfigs,
		TLSConfig:            tlsConfig,
	}
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

// ResolveEndpoint resolves the endpoint URL based on the target information
func (st *ScraperTask) ResolveEndpoint() (string, error) {
	// If it's a direct URL, just return it
	if st.TargetType == DirectURLType {
		return st.TargetURL, nil
	}

	// If it's a static endpoint, just return the target URL with the path
	if st.TargetType == StaticEndpointsType {
		if st.Path != "" && !strings.HasPrefix(st.Path, "/") {
			return fmt.Sprintf("%s/%s", st.TargetURL, st.Path), nil
		}
		return fmt.Sprintf("%s%s", st.TargetURL, st.Path), nil
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

		if st.Path != "" && !strings.HasPrefix(st.Path, "/") {
			return fmt.Sprintf("%s://%s:%d/%s", scheme, podIP, port, st.Path), nil
		}
		return fmt.Sprintf("%s://%s:%d%s", scheme, podIP, port, st.Path), nil
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

		if st.Path != "" && !strings.HasPrefix(st.Path, "/") {
			return fmt.Sprintf("%s://%s:%d/%s", scheme, endpointAddress, port, st.Path), nil
		}
		return fmt.Sprintf("%s://%s:%d%s", scheme, endpointAddress, port, st.Path), nil
	}

	return "", fmt.Errorf("unsupported target type: %s", st.TargetType)
}

// Run executes the scraper task
func (st *ScraperTask) Run() (*model.ScrapeRawData, error) {
	// Resolve the endpoint
	targetURL, resolveErr := st.ResolveEndpoint()
	if resolveErr != nil {
		if config.IsDebugEnabled() {
			logutil.Printf("DEBUG", "[DEBUG] Error resolving endpoint for target %s: %v", st.TargetName, resolveErr)
		}
		return nil, fmt.Errorf("error resolving endpoint for target %s: %v", st.TargetName, resolveErr)
	}

	// Format the URL
	formattedURL := client.FormatURL(targetURL)

	// Log detailed information if debug is enabled
	if config.IsDebugEnabled() {
		logutil.Printf("DEBUG", "[DEBUG] Starting scraper task for target [%s], URL [%s]", st.TargetName, targetURL)
		logutil.Printf("DEBUG", "[DEBUG] Formatted URL: %s", formattedURL)
		if st.TLSConfig != nil {
			logutil.Printf("DEBUG", "[DEBUG] Using TLS config with InsecureSkipVerify=%v", st.TLSConfig.InsecureSkipVerify)
		}
		if len(st.MetricRelabelConfigs) > 0 {
			logutil.Printf("DEBUG", "[DEBUG] Using %d metric relabel configs", len(st.MetricRelabelConfigs))
			for i, config := range st.MetricRelabelConfigs {
				logutil.Printf("DEBUG", "[DEBUG] Relabel config #%d: Action=%s, SourceLabels=%v, TargetLabel=%s, Regex=%s", 
					i+1, config.Action, config.SourceLabels, config.TargetLabel, config.Regex)
			}
		}
	}

	// Record start time for performance measurement if debug is enabled
	var startTime time.Time
	if config.IsDebugEnabled() {
		startTime = time.Now()
	}

	// Execute the HTTP request
	httpClient := client.GetInstance()
	var response string
	var httpErr error

	if st.TLSConfig != nil {
		response, httpErr = httpClient.ExecuteGetWithTLSConfig(formattedURL, st.TLSConfig)
	} else {
		response, httpErr = httpClient.ExecuteGet(formattedURL)
	}

	if httpErr != nil {
		if config.IsDebugEnabled() {
			logutil.Printf("DEBUG", "[DEBUG] Error scraping target %s for target %s: %v", targetURL, st.TargetName, httpErr)
		}
		return nil, fmt.Errorf("error scraping target %s for target %s: %v", targetURL, st.TargetName, httpErr)
	}

	// Create a ScrapeRawData instance with the response
	var rawData *model.ScrapeRawData
	if st.NodeName != "" && st.AddNodeLabel {
		rawData = model.NewScrapeRawDataWithNodeName(targetURL, response, st.MetricRelabelConfigs, st.NodeName, st.AddNodeLabel)
	} else {
		rawData = model.NewScrapeRawData(targetURL, response, st.MetricRelabelConfigs)
	}

	// Log detailed information if debug is enabled
	if config.IsDebugEnabled() {
		duration := time.Since(startTime)
		logutil.Printf("DEBUG", "[DEBUG] Scraper task completed for target [%s], URL [%s] in %v", st.TargetName, targetURL, duration)
		logutil.Printf("DEBUG", "[DEBUG] Response length: %d bytes", len(response))

		// Log a preview of the response (first 500 characters)
		preview := response
		if len(preview) > 500 {
			preview = preview[:500] + "..."
		}
		logutil.Printf("DEBUG", "[DEBUG] Response preview: %s", preview)

		// Count the number of metrics in the response (approximate)
		metricCount := 0
		for _, line := range strings.Split(response, "\n") {
			// Skip empty lines, comments, and metadata lines
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			metricCount++
		}
		logutil.Printf("DEBUG", "[DEBUG] Approximate number of metrics: %d", metricCount)
	}

	logutil.Printf("INFO", "ScraperTask: target [%s] fetched URL %s (length=%d)", st.TargetName, st.TargetURL, len(response))

	return rawData, nil
}
