package pkg

import (
	"fmt"
	"open-agent/pkg/model"
	"strings"
	"time"
)

// SampleDataSender is responsible for sending sample metrics data to the WHATAP server
type SampleDataSender struct {
	processedQueue chan *model.ConversionResult
}

// NewSampleDataSender creates a new SampleDataSender instance
func NewSampleDataSender(processedQueue chan *model.ConversionResult) *SampleDataSender {
	return &SampleDataSender{
		processedQueue: processedQueue,
	}
}

// SendSampleData sends the sample metrics data to the WHATAP server
func (s *SampleDataSender) SendSampleData() {
	// Create sample metrics data
	metrics := createSampleMetrics()
	
	// Create a conversion result with the sample metrics
	result := model.NewConversionResult(metrics, nil)
	
	// Send the conversion result to the processed queue
	s.processedQueue <- result
	
	fmt.Println("Sample data sent to the processed queue")
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