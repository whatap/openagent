package main

import (
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

func main() {
	http.HandleFunc("/probe/metrics/resource", handleMetrics)
	http.HandleFunc("/health", handleHealth)

	fmt.Println("Mock Azure Exporter starting on :8081")
	fmt.Println("Available endpoints:")
	fmt.Println("  - GET /probe/metrics/resource?subscription=...&target=...&metric=...")
	fmt.Println("  - GET /health")

	log.Fatal(http.ListenAndServe(":8081", nil))
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "OK")
}

func handleMetrics(w http.ResponseWriter, r *http.Request) {
	// Parse query parameters
	params := r.URL.Query()

	subscription := params.Get("subscription")
	target := params.Get("target")
	metric := params.Get("metric")
	interval := params.Get("interval")
	aggregation := params.Get("aggregation")

	// Log the request for debugging
	log.Printf("Request received with params: subscription=%s, target=%s, metric=%s, interval=%s, aggregation=%s",
		subscription, target, metric, interval, aggregation)

	// Set content type
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

	// Generate different metrics based on parameters
	timestamp := time.Now().Unix()

	if subscription == "" || target == "" || metric == "" {
		// Return error metrics if required parameters are missing
		fmt.Fprintf(w, "# HELP azure_exporter_error Error in Azure exporter\n")
		fmt.Fprintf(w, "# TYPE azure_exporter_error gauge\n")
		fmt.Fprintf(w, "azure_exporter_error{reason=\"missing_parameters\"} 1 %d\n", timestamp)
		return
	}

	// Extract resource type from target
	resourceType := extractResourceType(target)

	// Generate metrics based on the metric parameter
	metrics := strings.Split(metric, ",")

	for _, m := range metrics {
		m = strings.TrimSpace(m)
		switch m {
		case "avg_cpu_percent":
			fmt.Fprintf(w, "# HELP azure_sql_avg_cpu_percent Average CPU percentage\n")
			fmt.Fprintf(w, "# TYPE azure_sql_avg_cpu_percent gauge\n")
			fmt.Fprintf(w, "azure_sql_avg_cpu_percent{subscription=\"%s\",resource_type=\"%s\",aggregation=\"%s\",interval=\"%s\"} %.2f %d\n",
				subscription, resourceType, aggregation, interval, 45.67, timestamp)

		case "virtual_core_count":
			fmt.Fprintf(w, "# HELP azure_sql_virtual_core_count Virtual core count\n")
			fmt.Fprintf(w, "# TYPE azure_sql_virtual_core_count gauge\n")
			fmt.Fprintf(w, "azure_sql_virtual_core_count{subscription=\"%s\",resource_type=\"%s\",aggregation=\"%s\",interval=\"%s\"} %d %d\n",
				subscription, resourceType, aggregation, interval, 4, timestamp)

		case "memory_usage_percent":
			fmt.Fprintf(w, "# HELP azure_sql_memory_usage_percent Memory usage percentage\n")
			fmt.Fprintf(w, "# TYPE azure_sql_memory_usage_percent gauge\n")
			fmt.Fprintf(w, "azure_sql_memory_usage_percent{subscription=\"%s\",resource_type=\"%s\",aggregation=\"%s\",interval=\"%s\"} %.2f %d\n",
				subscription, resourceType, aggregation, interval, 78.34, timestamp)

		case "storage_usage_bytes":
			fmt.Fprintf(w, "# HELP azure_sql_storage_usage_bytes Storage usage in bytes\n")
			fmt.Fprintf(w, "# TYPE azure_sql_storage_usage_bytes gauge\n")
			fmt.Fprintf(w, "azure_sql_storage_usage_bytes{subscription=\"%s\",resource_type=\"%s\",aggregation=\"%s\",interval=\"%s\"} %d %d\n",
				subscription, resourceType, aggregation, interval, 1073741824, timestamp)

		default:
			// Unknown metric
			fmt.Fprintf(w, "# HELP azure_unknown_metric Unknown metric requested\n")
			fmt.Fprintf(w, "# TYPE azure_unknown_metric gauge\n")
			fmt.Fprintf(w, "azure_unknown_metric{subscription=\"%s\",resource_type=\"%s\",metric_name=\"%s\",aggregation=\"%s\",interval=\"%s\"} 0 %d\n",
				subscription, resourceType, m, aggregation, interval, timestamp)
		}
	}

	// Add some general exporter metrics
	fmt.Fprintf(w, "# HELP azure_exporter_scrape_duration_seconds Time spent scraping Azure API\n")
	fmt.Fprintf(w, "# TYPE azure_exporter_scrape_duration_seconds gauge\n")
	fmt.Fprintf(w, "azure_exporter_scrape_duration_seconds{subscription=\"%s\"} %.3f %d\n", subscription, 0.234, timestamp)

	fmt.Fprintf(w, "# HELP azure_exporter_scrape_success Whether the scrape was successful\n")
	fmt.Fprintf(w, "# TYPE azure_exporter_scrape_success gauge\n")
	fmt.Fprintf(w, "azure_exporter_scrape_success{subscription=\"%s\"} 1 %d\n", subscription, timestamp)
}

func extractResourceType(target string) string {
	// Extract resource type from Azure resource path
	// Example: /subscriptions/.../providers/Microsoft.Sql/managedInstances/...
	if strings.Contains(target, "Microsoft.Sql/managedInstances") {
		return "sql_managed_instance"
	} else if strings.Contains(target, "Microsoft.Sql/servers") {
		return "sql_server"
	} else if strings.Contains(target, "Microsoft.Compute/virtualMachines") {
		return "virtual_machine"
	} else if strings.Contains(target, "Microsoft.Storage/storageAccounts") {
		return "storage_account"
	}
	return "unknown"
}
