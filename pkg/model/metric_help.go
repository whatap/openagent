package model

// METRIC_HELP is a map of metric names to help text
var METRIC_HELP = map[string]string{
	"apiserver_request_duration_seconds_count": "Response latency distribution in seconds for each verb, dry run value, group, version, resource, subresource, scope and component.",
	"http_requests_total":                      "Total number of HTTP requests.",
	"http_requests_duration_seconds":           "Duration of HTTP requests in seconds.",
	"http_requests_in_progress":                "Number of currently active HTTP requests.",
	"http_requests_failed_total":               "Total number of failed HTTP requests.",
	"http_requests_success_total":              "Total number of successful HTTP requests.",
	"cpu_usage_seconds_total":                  "Total CPU usage in seconds.",
	"cpu_load_average_1m":                      "CPU load average over the last 1 minute.",
	"cpu_load_average_5m":                      "CPU load average over the last 5 minutes.",
	"cpu_load_average_15m":                     "CPU load average over the last 15 minutes.",
	"cpu_temperature_celsius":                  "Current CPU temperature in Celsius.",
	"memory_usage_bytes":                       "Total memory usage in bytes.",
	"memory_free_bytes":                        "Free memory in bytes.",
	"memory_available_bytes":                   "Available memory in bytes.",
	"memory_swap_used_bytes":                   "Used swap memory in bytes.",
	"memory_page_faults_total":                 "Total number of memory page faults.",
	"disk_read_bytes_total":                    "Total number of bytes read from disk.",
	"disk_write_bytes_total":                   "Total number of bytes written to disk.",
	"disk_reads_completed_total":               "Total number of disk reads completed.",
	"disk_writes_completed_total":              "Total number of disk writes completed.",
	"disk_inodes_total":                        "Total number of inodes on disk.",
	"disk_inodes_free":                         "Total number of free inodes on disk.",
	"network_transmit_bytes_total":             "Total bytes transmitted over the network.",
	"network_receive_bytes_total":              "Total bytes received over the network.",
	"network_transmit_packets_total":           "Total network packets transmitted.",
	"network_receive_packets_total":            "Total network packets received.",
	"process_cpu_seconds_total":                "Total CPU usage of the process in seconds.",
	"process_memory_usage_bytes":               "Memory usage of the process in bytes.",
	"process_open_fds":                         "Number of open file descriptors.",
	"process_max_fds":                          "Maximum number of file descriptors.",
	"process_threads_total":                    "Total number of threads.",
	"database_queries_total":                   "Total number of database queries executed.",
	"database_queries_duration_seconds":        "Total duration of database queries in seconds.",
	"database_queries_failed_total":            "Total number of failed database queries.",
	"database_rows_read_total":                 "Total number of rows read from the database.",
	"database_rows_written_total":              "Total number of rows written to the database.",
	"kafka_messages_in_total":                  "Total number of messages received by Kafka.",
	"kafka_messages_out_total":                 "Total number of messages sent by Kafka.",
	"kafka_producer_records_total":             "Total number of records produced by Kafka.",
	"kafka_consumer_lag_seconds":               "Kafka consumer lag in seconds.",
	"redis_commands_processed_total":           "Total number of Redis commands processed.",
	"redis_connections_active":                 "Total number of active connections to the Redis server.",
	"redis_memory_used_bytes":                  "Total memory used by Redis in bytes.",
	"redis_evicted_keys_total":                 "Total number of evicted keys due to memory limitations.",
	"redis_hit_ratio":                          "Redis cache hit ratio.",
	"redis_misses_total":                       "Total number of Redis cache misses.",
	"jvm_memory_used_bytes":                    "JVM memory usage in bytes.",
	"jvm_memory_max_bytes":                     "Maximum JVM memory in bytes.",
	"jvm_gc_collection_seconds_total":          "Total time spent in JVM GC collections in seconds.",
	"jvm_threads_live":                         "Number of live JVM threads.",
	"jvm_threads_peak":                         "Peak number of threads used by the JVM.",
	"jvm_classes_loaded":                       "Total number of loaded JVM classes.",
	"jvm_classes_unloaded_total":               "Total number of classes unloaded by the JVM.",
	"jvm_uptime_seconds":                       "JVM uptime in seconds.",
	"http_request_size_bytes":                  "Total size of HTTP request in bytes.",
	"http_response_size_bytes":                 "Total size of HTTP response in bytes.",
}

// METRIC_TYPE is a map of metric names to metric types
var METRIC_TYPE = map[string]string{

	"apiserver_request_duration_seconds_count": "counter",
	// HTTP 요청 관련 메트릭
	"http_requests_total":            "counter",
	"http_requests_duration_seconds": "histogram",
	"http_requests_in_progress":      "gauge",
	"http_requests_failed_total":     "counter",
	"http_requests_success_total":    "counter",

	// CPU 관련 메트릭
	"cpu_usage_seconds_total": "counter",
	"cpu_load_average_1m":     "gauge",
	"cpu_load_average_5m":     "gauge",
	"cpu_load_average_15m":    "gauge",
	"cpu_temperature_celsius": "gauge",

	// 메모리 관련 메트릭
	"memory_usage_bytes":       "gauge",
	"memory_free_bytes":        "gauge",
	"memory_available_bytes":   "gauge",
	"memory_swap_used_bytes":   "gauge",
	"memory_page_faults_total": "counter",

	// 디스크 관련 메트릭
	"disk_read_bytes_total":       "counter",
	"disk_write_bytes_total":      "counter",
	"disk_reads_completed_total":  "counter",
	"disk_writes_completed_total": "counter",
	"disk_inodes_total":           "gauge",
	"disk_inodes_free":            "gauge",

	// 네트워크 관련 메트릭
	"network_transmit_bytes_total":   "counter",
	"network_receive_bytes_total":    "counter",
	"network_transmit_packets_total": "counter",
	"network_receive_packets_total":  "counter",

	// 프로세스 관련 메트릭
	"process_cpu_seconds_total":  "counter",
	"process_memory_usage_bytes": "gauge",
	"process_open_fds":           "gauge",
	"process_max_fds":            "gauge",
	"process_threads_total":      "gauge",

	// 데이터베이스 관련 메트릭
	"database_queries_total":            "counter",
	"database_queries_duration_seconds": "histogram",
	"database_queries_failed_total":     "counter",
	"database_rows_read_total":          "counter",
	"database_rows_written_total":       "counter",

	// Kafka 관련 메트릭
	"kafka_messages_in_total":      "counter",
	"kafka_messages_out_total":     "counter",
	"kafka_producer_records_total": "counter",
	"kafka_consumer_lag_seconds":   "gauge",

	// Redis 관련 메트릭
	"redis_commands_processed_total": "counter",
	"redis_connections_active":       "gauge",
	"redis_memory_used_bytes":        "gauge",
	"redis_evicted_keys_total":       "counter",
	"redis_hit_ratio":                "gauge",
	"redis_misses_total":             "counter",

	// JVM 관련 메트릭
	"jvm_memory_used_bytes":           "gauge",
	"jvm_memory_max_bytes":            "gauge",
	"jvm_gc_collection_seconds_total": "counter",
	"jvm_threads_live":                "gauge",
	"jvm_threads_peak":                "gauge",
	"jvm_classes_loaded":              "gauge",
	"jvm_classes_unloaded_total":      "counter",
	"jvm_uptime_seconds":              "counter",

	// HTTP 요청 크기와 응답 크기 관련 메트릭
	"http_request_size_bytes":  "histogram",
	"http_response_size_bytes": "histogram",
}

// GetMetricHelp returns the help text for a metric
func GetMetricHelp(metric string) string {
	return METRIC_HELP[metric]
}

// GetMetricType returns the type for a metric
func GetMetricType(metric string) string {
	return METRIC_TYPE[metric]
}
