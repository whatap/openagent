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
var deltaMap = make(map[string]int64)

// Last time help information was sent
var lastHelpSendTime int64 = 0

// promaxLogMessage logs a message to both the file logger and stdout
func promaxLogMessage(logger *logfile.FileLogger, tag string, message string) {
	logger.Println(tag, message)
	fmt.Printf("[%s] %s\n", tag, message)
}

func main() {
	// Check if environment variables are set
	license := os.Getenv("WHATAP_LICENSE")
	host := os.Getenv("WHATAP_HOST")
	port := os.Getenv("WHATAP_PORT")
	license = "x41pl22ek7jhv-z43cebasdv4il7-z62p3l35fj5502"
	host = "15.165.146.117"
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
	promaxLogMessage(logger, "PromaX", "Starting PromaX sample sender")

	// Initialize secure communication
	servers := []string{fmt.Sprintf("%s:%s", host, port)}
	secure.StartNet(secure.WithLogger(logger), secure.WithAccessKey(license), secure.WithServers(servers), secure.WithOname("test"))

	// Initialize random number generator
	rand.Seed(time.Now().UnixNano())

	// Process metrics periodically
	for {
		process(logger)
		time.Sleep(10 * time.Second)
	}
}

// process creates and sends metrics and help information
func process(logger *logfile.FileLogger) {
	// Create metrics
	metrics := createMetrics()
	promaxLogMessage(logger, "PromaX", fmt.Sprintf("Created %d metrics", len(metrics)))

	// Create help information if needed
	helpItems := make([]*model.OpenMxHelp, 0)
	//startTime, _ := time.Parse("2006/01/02 15:04:05", "2025/07/11 10:45:00")
	now := time.Now().UnixMilli()

	// Send help information only once per minute
	if now-lastHelpSendTime > 5*dateutil.MILLIS_PER_SECOND {
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
			//fmt.Printf("mxh-Metric=%v\n", mxh.Metric)
			helpItems = append(helpItems, mxh)
		}
		lastHelpSendTime = now
	}

	// Send help information if available
	if len(helpItems) > 0 {
		helpPack := model.NewOpenMxHelpPack()
		securityMaster := secure.GetSecurityMaster()
		if securityMaster == nil {
			promaxLogMessage(logger, "PromaX", "No security master available")
			return
		}
		// Set PCODE and OID
		helpPack.SetPCODE(securityMaster.PCODE)
		helpPack.SetOID(securityMaster.OID)
		helpPack.SetTime(now)
		helpPack.SetRecords(helpItems)
		//testRe := helpPack.GetRecords()

		//fmt.Printf("helpPack.GetRecords()=%v\n", testRe)
		// Get the security master
		promaxLogMessage(logger, "PromaX", fmt.Sprintf("Sending %d help records", len(helpItems)))
		//for _, helpItem := range helpItems {
		//fmt.Printf("helpItem.Metric=%v//help=%v//type=%v\n", helpItem.Metric, helpItem.Get("help"), helpItem.Get("type"))
		//}
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
		promaxLogMessage(logger, "PromaX", "No security master available")
		return
	}

	//Set PCODE and OID
	metricsPack.SetPCODE(securityMaster.PCODE)
	metricsPack.SetOID(securityMaster.OID)

	promaxLogMessage(logger, "PromaX", fmt.Sprintf("Sending %d metrics", len(metrics)))
	secure.Send(secure.NET_SECURE_HIDE, metricsPack, true)
}

// createMetrics creates sample metrics data
func createMetrics() []*model.OpenMx {
	metrics := make([]*model.OpenMx, 0, 1100)

	// Add metrics with no labels
	promaxAddNoLabelMetrics(&metrics)

	// Add metrics with one label
	//promaxAddOneLabelsMetrics(&metrics)

	// Add metrics with two labels
	//promaxAddTwoLabelsMetrics(&metrics)

	// Add high cardinality metrics to reach 1000+ combinations
	//promaxAddHighCardinalityMetrics(&metrics)

	return metrics
}

// addDelta adds a random delta to the value for a metric
func addDelta(metricName string, value float64) float64 {
	// Generate a random delta between 0 and 99
	delta := rand.Int63n(100)

	// Add the delta to the stored value for this metric
	if _, ok := deltaMap[metricName]; !ok {
		deltaMap[metricName] = 0
	}
	deltaMap[metricName] += delta

	// Return the value plus the accumulated delta
	return value + float64(deltaMap[metricName])
}

// promaxAddNoLabelMetrics adds metrics with no labels
func promaxAddNoLabelMetrics(metrics *[]*model.OpenMx) {
	noLabelData := []struct {
		name  string
		value float64
	}{
		{"http_requests_total", 1523},
		{"http_requests_duration_seconds", 0.234},
		{"http_requests_in_progress", 37},
		{"http_requests_failed_total", 145},
		{"http_requests_success_total", 1378},
		{"cpu_usage_seconds_total", 78456},
		{"cpu_load_average_1m", 2.5},
		{"cpu_load_average_5m", 1.8},
		{"cpu_load_average_15m", 1.2},
		{"cpu_temperature_celsius", 55.3},
		{"memory_usage_bytes", 104857600},
		{"memory_free_bytes", 524288000},
		{"memory_available_bytes", 314572800},
		{"memory_swap_used_bytes", 20971520},
		{"memory_page_faults_total", 845321},
		{"disk_read_bytes_total", 5832145},
		{"disk_write_bytes_total", 4123654},
		{"disk_reads_completed_total", 14578},
		{"disk_writes_completed_total", 13854},
		{"network_transmit_bytes_total", 248930124},
		{"network_receive_bytes_total", 175435678},
		{"network_transmit_packets_total", 78932},
		{"network_receive_packets_total", 65421},
		{"process_cpu_seconds_total", 9854},
		{"process_memory_usage_bytes", 786432000},
		{"process_open_fds", 231},
		{"process_max_fds", 1024},
		{"process_threads_total", 34},
		{"database_queries_total", 56412},
		{"database_queries_duration_seconds", 0.056},
		{"database_queries_failed_total", 452},
		{"database_rows_read_total", 35621},
		{"database_rows_written_total", 19876},
		{"kafka_messages_in_total", 152000},
		{"kafka_messages_out_total", 145789},
		{"kafka_producer_records_total", 58746},
		{"kafka_consumer_lag_seconds", 3.2},
		{"redis_commands_processed_total", 12345678},
		{"redis_connections_active", 487},
		{"redis_memory_used_bytes", 167772160},
		{"redis_evicted_keys_total", 287},
		{"redis_hit_ratio", 0.89},
		{"redis_misses_total", 4521},
		{"jvm_memory_used_bytes", 786432000},
		{"jvm_memory_max_bytes", 2147483648},
		{"jvm_gc_collection_seconds_total", 120.3},
		{"jvm_threads_live", 145},
		{"jvm_threads_peak", 189},
		{"jvm_classes_loaded", 45210},
		{"jvm_classes_unloaded_total", 3241},
		{"jvm_uptime_seconds", 172800},
		{"http_request_size_bytes", 1783},
		{"http_response_size_bytes", 3456},
	}

	for _, data := range noLabelData {
		// Add a random delta to the value
		value := addDelta(data.name, data.value)

		// Create the metric with the current timestamp
		metric := model.NewOpenMxWithCurrentTime(data.name, value)
		*metrics = append(*metrics, metric)
	}
}

// promaxAddOneLabelsMetrics adds metrics with one label
func promaxAddOneLabelsMetrics(metrics *[]*model.OpenMx) {
	oneLabelData := []struct {
		name   string
		labels []string
		value  float64
	}{
		{"apiserver_request_total", []string{"target=kube-apiserver"}, 2999},
		{"apiserver_request_duration_seconds_count", []string{"target=kube-apiserver"}, 2999},
		{"http_requests_total", []string{"method=GET"}, 1023},
		{"http_requests_total", []string{"method=POST"}, 234},
		{"http_requests_failed_total", []string{"method=DELETE"}, 54},
		{"cpu_usage_seconds_total", []string{"core=0"}, 43212},
		{"cpu_load_average_1m", []string{"region=us-east"}, 3.4},
		{"memory_usage_bytes", []string{"host=server1"}, 509715200},
		{"memory_free_bytes", []string{"host=server2"}, 612345678},
		{"disk_read_bytes_total", []string{"device=sda"}, 3214587},
		{"disk_write_bytes_total", []string{"device=sdb"}, 8976543},
		{"network_transmit_bytes_total", []string{"interface=eth0"}, 112000000},
		{"network_receive_bytes_total", []string{"interface=eth1"}, 65432100},
		{"process_cpu_seconds_total", []string{"pid=1245"}, 5321},
		{"process_memory_usage_bytes", []string{"app=nginx"}, 298765432},
		{"database_queries_total", []string{"db=production"}, 21034},
		{"database_queries_total", []string{"db=staging"}, 8754},
		{"kafka_messages_in_total", []string{"topic=events"}, 72000},
		{"kafka_messages_out_total", []string{"topic=logs"}, 61500},
		{"redis_commands_processed_total", []string{"instance=cache1"}, 653214},
		{"redis_memory_used_bytes", []string{"instance=cache2"}, 198765432},
		{"jvm_memory_used_bytes", []string{"area=heap"}, 456123987},
		{"jvm_memory_max_bytes", []string{"area=non-heap"}, 987654321},
		{"jvm_gc_collection_seconds_total", []string{"collector=G1GC"}, 145.7},
		{"jvm_threads_live", []string{"type=daemon"}, 98},
		{"jvm_classes_loaded", []string{"app=myApp"}, 32145},
		{"jvm_uptime_seconds", []string{"host=server3"}, 275400},
		{"http_request_size_bytes", []string{"api=/login"}, 2093},
		{"http_response_size_bytes", []string{"api=/user/info"}, 4872},
		{"disk_inodes_total", []string{"filesystem=ext4"}, 3456789},
		{"disk_inodes_free", []string{"filesystem=xfs"}, 2765432},
	}

	for _, data := range oneLabelData {
		// Create a unique key for the metric with its labels
		key := data.name
		for _, label := range data.labels {
			key += "_" + label
		}

		// Add a random delta to the value
		value := addDelta(key, data.value)

		// Create the metric with the current timestamp
		metric := model.NewOpenMxWithCurrentTime(data.name, value)

		// Add labels
		for _, labelStr := range data.labels {
			parts := promaxSplitLabel(labelStr)
			if len(parts) == 2 {
				metric.AddLabel(parts[0], parts[1])
			}
		}

		*metrics = append(*metrics, metric)
	}
}

// promaxAddTwoLabelsMetrics adds metrics with two labels
func promaxAddTwoLabelsMetrics(metrics *[]*model.OpenMx) {
	twoLabelData := []struct {
		name   string
		labels []string
		value  float64
	}{
		{"apiserver_request_duration_seconds_count", []string{"target=kube-apiserver", "instance=192.168.0.105"}, 3333},
		{"http_requests_total", []string{"method=GET", "status=200"}, 982},
		{"http_requests_total", []string{"method=POST", "status=500"}, 45},
		{"cpu_usage_seconds_total", []string{"core=1", "node=node1"}, 65478},
		{"memory_usage_bytes", []string{"host=server2", "region=us-east"}, 312457600},
		{"disk_read_bytes_total", []string{"device=sdb", "mount=/data"}, 9876543},
		{"network_transmit_bytes_total", []string{"interface=eth1", "speed=1Gbps"}, 187654321},
		{"process_cpu_seconds_total", []string{"pid=2378", "app=nginx"}, 7421},
		{"jvm_memory_used_bytes", []string{"area=non-heap", "pool=Metaspace"}, 298765432},
		{"database_queries_total", []string{"db=staging", "type=select"}, 19283},
		{"kafka_messages_in_total", []string{"topic=logs", "partition=3"}, 15423},
	}

	for _, data := range twoLabelData {
		// Create a unique key for the metric with its labels
		key := data.name
		for _, label := range data.labels {
			key += "_" + label
		}

		// Add a random delta to the value
		value := addDelta(key, data.value)

		// Create the metric with the current timestamp
		metric := model.NewOpenMxWithCurrentTime(data.name, value)

		// Add labels
		for _, labelStr := range data.labels {
			parts := promaxSplitLabel(labelStr)
			if len(parts) == 2 {
				metric.AddLabel(parts[0], parts[1])
			}
		}

		*metrics = append(*metrics, metric)
	}
}

// promaxAddHighCardinalityMetrics adds metrics with high cardinality to reach 1000+ combinations

// Helper function to split a label string in the format "key=value" into key and value
func promaxSplitLabel(label string) []string {
	idx := strings.Index(label, "=")
	if idx == -1 {
		return []string{}
	}
	return []string{label[:idx], label[idx+1:]}
}

// promaxAddHighCardinalityMetrics adds metrics with high cardinality to reach 1000+ combinations
func promaxAddHighCardinalityMetrics(metrics *[]*model.OpenMx) {
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
	}

	// Label keys
	labelKeys := []string{
		"service", "instance", "endpoint", "method", "status", "version",
		"region", "zone", "cluster", "namespace", "pod", "container",
		"host", "node", "datacenter", "environment", "tier", "job",
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
	}

	// Count of metrics added
	count := 0
	targetCount := 1000 - len(*metrics)

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
			numCombinations := 10 // Adjust this to control how many combinations per metric
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
				value := addDelta(key, baseValue)

				// Create the metric with the current timestamp
				metric := model.NewOpenMxWithCurrentTime(metricName, value)

				// Add labels
				for _, labelStr := range labels {
					parts := promaxSplitLabel(labelStr)
					if len(parts) == 2 {
						metric.AddLabel(parts[0], parts[1])
					}
				}

				*metrics = append(*metrics, metric)
				count++
			}
		}
	}

	promaxLogMessage(nil, "PromaX", fmt.Sprintf("Added %d high cardinality metrics", count))
}
