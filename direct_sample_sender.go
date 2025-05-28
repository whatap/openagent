package main

import (
	"fmt"
	"github.com/whatap/gointernal/net/secure"
	"github.com/whatap/golib/logger/logfile"
	"open-agent/pkg/model"
	"os"
	"strings"
	"time"
)

// logBoth logs a message to both the file logger and stdout
func logBoth(logger *logfile.FileLogger, tag string, message string) {
	logger.Println(tag, message)
	fmt.Printf("[%s] %s\n", tag, message)
}

func main() {
	// Check if environment variables are set
	license := os.Getenv("WHATAP_LICENSE")
	host := os.Getenv("WHATAP_HOST")
	port := os.Getenv("WHATAP_PORT")

	if license == "" || host == "" || port == "" {
		fmt.Println("Please set the following environment variables:")
		fmt.Println("WHATAP_LICENSE - The license key for the WHATAP server")
		fmt.Println("WHATAP_HOST - The hostname or IP address of the WHATAP server")
		fmt.Println("WHATAP_PORT - The port number of the WHATAP server")
		os.Exit(1)
	}

	// Create a logger
	logger := logfile.NewFileLogger()
	logBoth(logger, "DirectSampleSender", "Starting direct sample sender")

	// Initialize secure communication
	servers := []string{fmt.Sprintf("%s:%s", host, port)}

	secure.StartNet(secure.WithLogger(logger), secure.WithAccessKey(license), secure.WithServers(servers), secure.WithOname("test"))

	// Create sample metrics data
	metrics := createSampleMetrics()

	// Send the sample data directly
	logBoth(logger, "DirectSampleSender", "Sending sample data to WHATAP server")
	sendSampleData(metrics, logger)

	logBoth(logger, "DirectSampleSender", "Sample data sent successfully")
}

// sendSampleData sends the sample metrics data directly to the WHATAP server
func sendSampleData(metrics []*model.OpenMx, logger *logfile.FileLogger) {

	// Create a pack for the metrics
	metricsPack := model.NewOpenMxPack()
	metricsPack.SetRecords(metrics)

	// Set the time to the current time
	metricsPack.SetTime(time.Now().UnixMilli())

	// Get the security master from the secure package
	securityMaster := secure.GetSecurityMaster()
	if securityMaster == nil {
		logBoth(logger, "DirectSampleSender", "No security master available")
		return
	}

	// Set the PCODE and OID from the security master
	metricsPack.SetPCODE(securityMaster.PCODE)
	metricsPack.SetOID(securityMaster.OID)

	// Log the metrics
	logBoth(logger, "DirectSampleSender", fmt.Sprintf("PCODE=%d", securityMaster.PCODE))
	logBoth(logger, "DirectSampleSender", fmt.Sprintf("Sending %d metrics", len(metrics)))

	// Send the pack to the server using secure.Send
	secure.Send(secure.NET_SECURE_HIDE, metricsPack, true)

	logBoth(logger, "DirectSampleSender", "Metrics sent successfully")

	// Create help information for the metrics
	helpList := createHelpInfo(metrics)
	if len(helpList) > 0 {
		// Create a pack for the help information
		helpPack := model.NewOpenMxHelpPack()
		helpPack.SetRecords(helpList)

		// Set the time to the current time
		helpPack.SetTime(time.Now().UnixMilli())

		// Set the PCODE and OID from the security master
		helpPack.SetPCODE(securityMaster.PCODE)
		helpPack.SetOID(securityMaster.OID)

		// Log the help information
		logBoth(logger, "DirectSampleSender", fmt.Sprintf("Sending %d help records", len(helpList)))

		// Send the pack to the server using secure.Send
		secure.Send(secure.NET_SECURE_HIDE, helpPack, true)

		logBoth(logger, "DirectSampleSender", "Help information sent successfully")
	}
}

// createHelpInfo creates help information for the metrics
func createHelpInfo(metrics []*model.OpenMx) []*model.OpenMxHelp {
	// Create a map to store unique metrics
	uniqueMetrics := make(map[string]bool)
	for _, metric := range metrics {
		uniqueMetrics[metric.Metric] = true
	}

	// Create help information for each unique metric
	helpList := make([]*model.OpenMxHelp, 0, len(uniqueMetrics))
	for metric := range uniqueMetrics {

		help := model.NewOpenMxHelp(metric)
		help.Put("help", fmt.Sprintf("Help information for %s", metric))
		help.Put("type", "gauge")
		helpList = append(helpList, help)
	}

	return helpList
}

// createSampleMetrics creates sample metrics data based on the provided examples
func createSampleMetrics() []*model.OpenMx {
	now := time.Now().UnixMilli()
	metrics := make([]*model.OpenMx, 0, 100)

	// Add metrics with no labels
	addNoLabelMetrics(&metrics, now)

	// Add metrics with one label
	addOneLabelsMetrics(&metrics, now)

	// Add metrics with two labels
	addTwoLabelsMetrics(&metrics, now)

	return metrics
}

// addNoLabelMetrics adds metrics with no labels
func addNoLabelMetrics(metrics *[]*model.OpenMx, timestamp int64) {
	noLabelData := []struct {
		name  string
		value float64
	}{
		{"attp_requests_total", 1523},
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
		metric := model.NewOpenMx(data.name, timestamp, data.value)
		*metrics = append(*metrics, metric)
	}
}

// addOneLabelsMetrics adds metrics with one label
func addOneLabelsMetrics(metrics *[]*model.OpenMx, timestamp int64) {
	oneLabelData := []struct {
		name   string
		labels []string
		value  float64
	}{
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
		metric := model.NewOpenMx(data.name, timestamp, data.value)
		for _, labelStr := range data.labels {
			parts := splitLabel(labelStr)
			if len(parts) == 2 {
				metric.AddLabel(parts[0], parts[1])
			}
		}
		*metrics = append(*metrics, metric)
	}
}

// addTwoLabelsMetrics adds metrics with two labels
func addTwoLabelsMetrics(metrics *[]*model.OpenMx, timestamp int64) {
	twoLabelData := []struct {
		name   string
		labels []string
		value  float64
	}{
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
		metric := model.NewOpenMx(data.name, timestamp, data.value)
		for _, labelStr := range data.labels {
			parts := splitLabel(labelStr)
			if len(parts) == 2 {
				metric.AddLabel(parts[0], parts[1])
			}
		}
		*metrics = append(*metrics, metric)
	}
}

// Helper function to split a label string in the format "key=value" into key and value
func splitLabel(label string) []string {
	idx := strings.Index(label, "=")
	if idx == -1 {
		return []string{}
	}
	return []string{label[:idx], label[idx+1:]}
}
