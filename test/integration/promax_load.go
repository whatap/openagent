package main

import (
	"fmt"
	"github.com/whatap/gointernal/net/secure"
	"github.com/whatap/golib/logger/logfile"
	"github.com/whatap/golib/util/dateutil"
	"math/rand"
	"open-agent/pkg/model"
	"os"
	"strings"
	"time"
)

// Map to store the last value for each metric to calculate deltas
var loadTestDeltaMap = make(map[string]int64)

// Last time help information was sent
var loadTestLastHelpSendTime int64 = 0

// logMessage logs a message to both the file logger and stdout
func logMessage(logger *logfile.FileLogger, tag string, message string) {
	if logger != nil {
		logger.Println(tag, message)
	}
	fmt.Printf("[%s] %s\n", tag, message)
}

func main() {
	// Check if environment variables are set
	license := os.Getenv("WHATAP_LICENSE")
	host := os.Getenv("WHATAP_HOST")
	port := os.Getenv("WHATAP_PORT")
	license = "x22gg93735j9v-z63jpk29lgtn68-x52sdl202an6h"
	host = "192.168.1.20"
	port = "6600"
	if license == "" || host == "" || port == "" {
		fmt.Println("Please set the following environment variables:")
		fmt.Println("WHATAP_LICENSE - The license key for the WHATAP server")
		fmt.Println("WHATAP_HOST - The hostname or IP address of the WHATAP server")
		fmt.Println("WHATAP_PORT - The port number of the WHATAP server")
		os.Exit(1)
	}

	// Create a logger
	logger := logfile.NewFileLogger()
	logMessage(logger, "LoadTest", "Starting Load Test with 1000+ metric combinations")

	// Initialize secure communication
	servers := []string{fmt.Sprintf("%s:%s", host, port)}
	secure.StartNet(secure.WithLogger(logger), secure.WithAccessKey(license), secure.WithServers(servers), secure.WithOname("load-test"))

	// Initialize random number generator
	rand.Seed(time.Now().UnixNano())

	// Process metrics periodically
	for {
		processLoadTest(logger)
		time.Sleep(10 * time.Second)
	}
}

// processLoadTest creates and sends metrics and help information
func processLoadTest(logger *logfile.FileLogger) {
	// Create metrics
	metrics := createLoadTestMetrics()
	logMessage(logger, "LoadTest", fmt.Sprintf("Created %d metrics", len(metrics)))

	// Create help information if needed
	helpItems := make([]*model.OpenMxHelp, 0)
	now := time.Now().UnixMilli()

	// Send help information only once per minute
	if now-loadTestLastHelpSendTime > 60*dateutil.MILLIS_PER_SECOND {
		for _, mx := range metrics {
			helpText := model.GetMetricHelp(mx.Metric)
			metricType := model.GetMetricType(mx.Metric)

			// Use default values if not found
			if helpText == "" {
				helpText = fmt.Sprintf("Help information for %s", mx.Metric)
			}
			if metricType == "" {
				metricType = "gauge"
			}

			mxh := model.NewOpenMxHelp(mx.Metric)
			mxh.Put("help", helpText)
			mxh.Put("type", metricType)
			helpItems = append(helpItems, mxh)
		}
		loadTestLastHelpSendTime = now
	}

	// Send help information if available
	if len(helpItems) > 0 {
		helpPack := model.NewOpenMxHelpPack()
		securityMaster := secure.GetSecurityMaster()
		if securityMaster == nil {
			logMessage(logger, "LoadTest", "No security master available")
			return
		}
		// Set PCODE and OID
		helpPack.SetPCODE(securityMaster.PCODE)
		helpPack.SetOID(securityMaster.OID)
		helpPack.SetTime(now)
		helpPack.SetRecords(helpItems)

		// Get the security master
		logMessage(logger, "LoadTest", fmt.Sprintf("Sending %d help records", len(helpItems)))
		secure.Send(secure.NET_SECURE_HIDE, helpPack, true)
		time.Sleep(1000 * time.Millisecond)
	}

	//Send metrics
	metricsPack := model.NewOpenMxPack()
	metricsPack.SetTime(now)
	metricsPack.SetRecords(metrics)

	// Get the security master
	securityMaster := secure.GetSecurityMaster()
	if securityMaster == nil {
		logMessage(logger, "LoadTest", "No security master available")
		return
	}

	//Set PCODE and OID
	metricsPack.SetPCODE(securityMaster.PCODE)
	metricsPack.SetOID(securityMaster.OID)

	logMessage(logger, "LoadTest", fmt.Sprintf("Sending %d metrics", len(metrics)))
	secure.Send(secure.NET_SECURE_HIDE, metricsPack, true)
}

// createLoadTestMetrics creates sample metrics data with high cardinality
func createLoadTestMetrics() []*model.OpenMx {
	metrics := make([]*model.OpenMx, 0, 1100)

	// Generate 1000+ high cardinality metrics
	generateHighCardinalityMetrics(&metrics, 1000)

	return metrics
}

// addDeltaValue adds a random delta to the value for a metric
func addDeltaValue(metricName string, value float64) float64 {
	// Generate a random delta between 0 and 99
	delta := rand.Int63n(100)

	// Add the delta to the stored value for this metric
	if _, ok := loadTestDeltaMap[metricName]; !ok {
		loadTestDeltaMap[metricName] = 0
	}
	loadTestDeltaMap[metricName] += delta

	// Return the value plus the accumulated delta
	return value + float64(loadTestDeltaMap[metricName])
}

// splitLabel splits a label string in the format "key=value" into key and value
func splitLabel(label string) []string {
	idx := strings.Index(label, "=")
	if idx == -1 {
		return []string{}
	}
	return []string{label[:idx], label[idx+1:]}
}

// generateHighCardinalityMetrics adds metrics with high cardinality to reach the target count
func generateHighCardinalityMetrics(metrics *[]*model.OpenMx, targetCount int) {
	// Base metric names
	baseMetrics := []string{
		"http_requests_total",
		"http_request_duration_seconds",
		"database_queries_total",
		"database_query_duration_seconds",
		"cache_hits_total",
		"cache_misses_total",
		"api_requests_total",
		"api_request_duration_seconds",
		"system_cpu_usage",
		"system_memory_usage",
		"network_transmit_bytes_total",
		"network_receive_bytes_total",
		"disk_read_bytes_total",
		"disk_write_bytes_total",
		"jvm_memory_used_bytes",
		"jvm_gc_collection_seconds_total",
		"process_cpu_seconds_total",
		"process_memory_usage_bytes",
		"container_cpu_usage_seconds_total",
		"container_memory_usage_bytes",
	}

	// Label keys
	labelKeys := []string{
		"service", "instance", "endpoint", "method", "status", "version",
		"region", "zone", "cluster", "namespace", "pod", "container",
		"host", "node", "datacenter", "environment", "tier", "job",
		"app", "team", "owner", "component", "shard", "partition", "replica",
	}

	// Label values for each key
	labelValues := map[string][]string{
		"service":     {"auth", "payment", "user", "order", "catalog", "cart", "shipping", "notification", "search", "recommendation"},
		"instance":    {"instance-1", "instance-2", "instance-3", "instance-4", "instance-5"},
		"endpoint":    {"/api/v1/users", "/api/v1/products", "/api/v1/orders", "/api/v1/payments", "/api/v1/auth", "/health", "/metrics"},
		"method":      {"GET", "POST", "PUT", "DELETE", "PATCH"},
		"status":      {"200", "201", "400", "401", "403", "404", "500", "503"},
		"version":     {"v1", "v2", "v3", "beta", "alpha"},
		"region":      {"us-east-1", "us-west-1", "eu-west-1", "ap-northeast-1", "ap-southeast-1"},
		"zone":        {"zone-a", "zone-b", "zone-c"},
		"cluster":     {"cluster-1", "cluster-2", "cluster-3"},
		"namespace":   {"default", "kube-system", "monitoring", "logging", "app"},
		"pod":         {"pod-1", "pod-2", "pod-3", "pod-4", "pod-5"},
		"container":   {"container-1", "container-2", "container-3"},
		"host":        {"host-1", "host-2", "host-3", "host-4", "host-5"},
		"node":        {"node-1", "node-2", "node-3", "node-4", "node-5"},
		"datacenter":  {"dc-1", "dc-2", "dc-3"},
		"environment": {"prod", "staging", "dev", "test"},
		"tier":        {"web", "app", "db", "cache", "worker"},
		"job":         {"scraper", "processor", "api", "worker", "scheduler"},
		"app":         {"frontend", "backend", "middleware", "database", "cache"},
		"team":        {"platform", "infrastructure", "application", "data", "security"},
		"owner":       {"team-a", "team-b", "team-c", "team-d", "team-e"},
		"component":   {"ui", "api", "service", "database", "cache"},
		"shard":       {"shard-1", "shard-2", "shard-3", "shard-4", "shard-5"},
		"partition":   {"partition-1", "partition-2", "partition-3", "partition-4", "partition-5"},
		"replica":     {"replica-1", "replica-2", "replica-3", "replica-4", "replica-5"},
	}

	// Count of metrics added
	count := 0

	// Generate metrics with 1, 2, and 3 labels
	for numLabels := 1; numLabels <= 3; numLabels++ {
		// For each base metric
		for _, metricName := range baseMetrics {
			// Skip if we've reached the target
			if count >= targetCount {
				break
			}

			// Select random label keys for this metric
			selectedLabelKeys := make([]string, 0, numLabels)
			availableLabelKeys := make([]string, len(labelKeys))
			copy(availableLabelKeys, labelKeys)

			for i := 0; i < numLabels; i++ {
				if len(availableLabelKeys) == 0 {
					break
				}
				// Select a random label key
				idx := rand.Intn(len(availableLabelKeys))
				selectedLabelKeys = append(selectedLabelKeys, availableLabelKeys[idx])

				// Remove the selected key to avoid duplicates
				availableLabelKeys = append(availableLabelKeys[:idx], availableLabelKeys[idx+1:]...)
			}

			// Generate combinations of label values
			numCombinations := 20 // Adjust this to control how many combinations per metric
			for i := 0; i < numCombinations; i++ {
				// Skip if we've reached the target
				if count >= targetCount {
					break
				}

				// Create labels for this metric
				labels := make([]string, 0, numLabels)
				for _, labelKey := range selectedLabelKeys {
					values := labelValues[labelKey]
					if len(values) > 0 {
						// Select a random value for this key
						valueIdx := rand.Intn(len(values))
						labels = append(labels, fmt.Sprintf("%s=%s", labelKey, values[valueIdx]))
					}
				}

				// Create a unique key for the metric with its labels
				key := metricName
				for _, label := range labels {
					key += "_" + label
				}

				// Generate a random base value
				baseValue := 100.0 + rand.Float64()*900.0

				// Add a random delta to the value
				value := addDeltaValue(key, baseValue)

				// Create the metric with the current timestamp
				metric := model.NewOpenMxWithCurrentTime(metricName, value)

				// Add labels
				for _, labelStr := range labels {
					parts := splitLabel(labelStr)
					if len(parts) == 2 {
						metric.AddLabel(parts[0], parts[1])
					}
				}

				*metrics = append(*metrics, metric)
				count++
			}
		}
	}

	logMessage(nil, "LoadTest", fmt.Sprintf("Generated %d high cardinality metrics", count))
}
