package open

import (
	"context"
	"fmt"
	"github.com/whatap/gointernal/net/secure"
	"github.com/whatap/golib/logger/logfile"
	"github.com/whatap/golib/util/dateutil"
	"math/rand"
	"open-agent/pkg/config"
	"open-agent/pkg/discovery"
	"open-agent/pkg/k8s"
	"open-agent/pkg/model"
	"open-agent/pkg/processor"
	"open-agent/pkg/scraper"
	"open-agent/pkg/sender"
	"open-agent/tools/util/logutil"
	"os"
	"strings"
	"time"
)

const (
	// QueueSize is the size of the queues
	RawQueueSize       = 10000
	ProcessedQueueSize = 10000
)

var isRun = false
var readyHealthCheck = false
var runDate int64

// Global logger for the application
var appLogger *logfile.FileLogger

// Channels for shutdown coordination
var shutdownCh = make(chan struct{})
var doneCh = make(chan struct{}, 3) // Buffer for 3 components: scraper, processor, sender

// Debug mode variables
// Map to store the last value for each metric to calculate deltas
var deltaMap = make(map[string]int64)

// Last time help information was sent
var lastHelpSendTime int64 = 0

// SetAppLogger sets the application logger
func SetAppLogger(logger *logfile.FileLogger) {
	appLogger = logger
}

// GetAppLogger returns the application logger
func GetAppLogger() *logfile.FileLogger {
	return appLogger
}

// BootOpenAgent initializes and starts the Prometheus Agent
func BootOpenAgent(version, commitHash string, logger *logfile.FileLogger) {
	// Store the logger in the global variable for centralized access
	SetAppLogger(logger)

	// Display version information prominently
	if version == "" {
		version = "dev"
	}
	if commitHash == "" {
		commitHash = "unknown"
	}

	logutil.Infof("START", "\nWHATAP Open Agent Starting\n")
	logutil.Infof("START", " Version: %s\n", version)
	logutil.Infof("START", " Build: %s\n", commitHash)
	logutil.Infof("START", " Started at: %s\n\n", time.Now().Format("2006-01-02 15:04:05 MST"))

	// Get configuration values using the config package
	servers := make([]string, 0)
	license := config.Get("WHATAP_LICENSE")
	hosts := config.Get("WHATAP_HOST")
	port := config.GetIntWithDefault("WHATAP_PORT", 6600)
	if license == "" || hosts == "" {
		logutil.Println("SETTING", "Please set the following configuration values:")
		logutil.Println("SETTING", "WHATAP_LICENSE - The license key for the WHATAP server")
		logutil.Println("SETTING", "WHATAP_HOST - The hostname or IP address of the WHATAP server")
		logutil.Println("SETTING", "WHATAP_PORT - The port number of the WHATAP server (default: 6600)")
		os.Exit(1)
	}

	hostSlice := strings.Split(hosts, "/")
	// Parse server list
	for _, hostSliced := range hostSlice {
		servers = append(servers, fmt.Sprintf("%s:%d", hostSliced, port))
	}

	// Set logger level based on debug configuration from whatap.conf
	if config.IsDebugEnabled() {
		logutil.SetLevel(0) // LOG_LEVEL_DEBUG = 0
		logutil.Infof("CONFIG", "Debug logging enabled from whatap.conf")
	} else {
		logutil.SetLevel(1) // LOG_LEVEL_INFO = 1
		logutil.Infof("CONFIG", "Debug logging disabled from whatap.conf")
	}

	// Initialize secure communication
	secure.StartNet(secure.WithLogger(logger), secure.WithAccessKey(license), secure.WithServers(servers), secure.WithOname("test"))

	// Check if debug mode is enabled
	debugMode := os.Getenv("debug")
	if debugMode == "true" {
		logutil.Infoln("BootOpenAgent", "Debug mode enabled, running process method")
		// Initialize random number generator
		rand.Seed(time.Now().UnixNano())
		// Run the process method in a loop
		for {
			process(logger)
			time.Sleep(10 * time.Second)
		}
		// The code below will not be executed in debug mode
	}

	logger.Infoln("BootOpenAgent-Debug mode disabled, starting agent")
	// Create channels for communication between components
	rawQueue := make(chan *model.ScrapeRawData, RawQueueSize)
	processedQueue := make(chan *model.ConversionResult, ProcessedQueueSize)

	// Create the configuration manager
	configManager := config.NewConfigManager()
	// Check if configManager is nil (which happens if the configuration file is missing)
	if configManager == nil {
		logutil.Infoln("BootOpenAgent", "Failed to create configuration manager. Please ensure scrape_config.yaml exists.")
		return
	}

	// Create service discovery
	serviceDiscovery := discovery.NewKubernetesDiscovery(configManager)

	// Start service discovery as an independent component
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logutil.Errorln("ServiceDiscoveryPanic", fmt.Sprintf("Recovered from panic: %v", r))
			}
		}()

		// Load targets from configuration
		scrapeConfigs := configManager.GetScrapeConfigs()
		if scrapeConfigs != nil {
			if err := serviceDiscovery.LoadTargets(scrapeConfigs); err != nil {
				logutil.Println("ServiceDiscovery", fmt.Sprintf("Failed to load targets: %v", err))
				return
			}

			// Start service discovery
			if err := serviceDiscovery.Start(context.Background()); err != nil {
				logutil.Println("ServiceDiscovery", fmt.Sprintf("Failed to start service discovery: %v", err))
				return
			}

			logutil.Infoln("ServiceDiscovery", "Service discovery started successfully")
		} else {
			logutil.Infoln("ServiceDiscovery", "No scrape configs found, service discovery not started")
		}
	}()

	// Create and start the scraper manager with error recovery and shutdown handling
	scraperManager := scraper.NewScraperManager(configManager, serviceDiscovery, rawQueue)

	// Configuration changes will be automatically reflected in the next scraping cycle
	logger.Infoln("BootOpenAgent", "ScraperManager will automatically use latest configuration")

	// ConfigManager automatically handles ConfigMap synchronization
	if !config.IsForceStandaloneMode() {
		k8sClient := k8s.GetInstance()
		if k8sClient.IsInitialized() {
			logger.Infoln("BootOpenAgent", "Kubernetes environment detected - ConfigManager handles ConfigMap synchronization")
		} else {
			logger.Infoln("BootOpenAgent", "Non-Kubernetes environment detected - ConfigManager watches scrape_config.yaml")
		}
	}

	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Infoln("ScraperManagerPanic", fmt.Sprintf("Recovered from panic: %v", r))
				// Restart the scraper manager after a short delay
				select {
				case <-shutdownCh:
					// Don't restart if we're shutting down
					doneCh <- struct{}{}
					return
				case <-time.After(5 * time.Second):
					go scraperManager.StartScraping()
				}
			} else {
				// Normal exit
				doneCh <- struct{}{}
			}
		}()

		// Start scraping in a separate goroutine so we can listen for shutdown
		scrapeDone := make(chan struct{})
		go func() {
			scraperManager.StartScraping()
			close(scrapeDone)
		}()

		// Wait for either scraping to finish or shutdown signal
		select {
		case <-scrapeDone:
			// Scraping finished normally
		case <-shutdownCh:
			// Shutdown requested, cleanup will be handled by defer
			logger.Println("ScraperManager", "Shutdown requested")
			// Here we would call a stop method on scraperManager if it had one
		}
	}()

	// Create and start the newProcessor with error recovery and shutdown handling
	newProcessor := processor.NewProcessor(rawQueue, processedQueue)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Println("ProcessorPanic", fmt.Sprintf("Recovered from panic: %v", r))
				// Restart the newProcessor after a short delay
				select {
				case <-shutdownCh:
					// Don't restart if we're shutting down
					doneCh <- struct{}{}
					return
				case <-time.After(5 * time.Second):
					newProcessor.Start()
				}
			} else {
				// Normal exit
				doneCh <- struct{}{}
			}
		}()

		// Start processing in a separate goroutine so we can listen for shutdown
		processDone := make(chan struct{})
		go func() {
			newProcessor.Start()
			close(processDone)
		}()

		// Wait for either processing to finish or shutdown signal
		select {
		case <-processDone:
			// Processing finished normally
		case <-shutdownCh:
			// Shutdown requested, cleanup will be handled by defer
			logger.Println("Processor", "Shutdown requested")
			// Here we would call a stop method on newProcessor if it had one
		}
	}()

	// Create and start the sender with error recovery and shutdown handling
	senderInstance = sender.NewSender(processedQueue, GetAppLogger())
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Println("SenderPanic", fmt.Sprintf("Recovered from panic: %v", r))
				// Restart the sender after a short delay
				select {
				case <-shutdownCh:
					// Don't restart if we're shutting down
					doneCh <- struct{}{}
					return
				case <-time.After(5 * time.Second):
					senderInstance.Start()
				}
			} else {
				// Normal exit
				doneCh <- struct{}{}
			}
		}()

		// Start sending in a separate goroutine so we can listen for shutdown
		sendDone := make(chan struct{})
		go func() {
			senderInstance.Start()
			close(sendDone)
		}()

		// Wait for either sending to finish or shutdown signal
		select {
		case <-sendDone:
			// Sending finished normally
		case <-shutdownCh:
			// Shutdown requested, cleanup will be handled by defer
			logger.Infoln("Sender", "Shutdown requested")
			// Call Stop on the sender
			senderInstance.Stop()
		}
	}()

	// Set flags to indicate the agent is running
	isRun = true
	runDate = dateutil.SystemNow()

	logger.Infoln("BootOpenAgent", "OpenAgent started successfully")
}

// IsOK checks if the agent is running properly
func IsOK() bool {
	// If health check is not ready yet, check if it's time to enable it
	if !readyHealthCheck {
		if isRun && (dateutil.SystemNow()-runDate > 2*dateutil.MILLIS_PER_MINUTE) {
			GetAppLogger().Println("HealthCheckReady", "Worker HealthCheck Ready")
			readyHealthCheck = true
		}
		return true // Return healthy until health check is ready
	}

	// Perform actual health check
	secu := secure.GetSecurityMaster()

	// Check PCODE
	if secu.PCODE == 0 {
		GetAppLogger().Println("HealthCheckFail", fmt.Sprintf("PCODE Error: %d", secu.PCODE))
		return false
	}

	// Check OID
	if secu.OID == 0 {
		GetAppLogger().Println("HealthCheckFail", fmt.Sprintf("OID Error: %d", secu.OID))
		return false
	}

	return true
}

// Global variables to store component references for shutdown
var senderInstance *sender.Sender

// Shutdown gracefully shuts down all components
func Shutdown() {
	if !isRun {
		GetAppLogger().Println("Shutdown", "Agent is not running")
		return
	}

	GetAppLogger().Println("Shutdown", "Initiating graceful shutdown")

	// Stop the sender if it exists
	if senderInstance != nil {
		GetAppLogger().Println("Shutdown", "Stopping sender")
		senderInstance.Stop()
	}

	// Signal all components to shut down
	close(shutdownCh)

	// Wait for all components to acknowledge shutdown with a timeout
	timeout := time.NewTimer(30 * time.Second)
	defer timeout.Stop()

	// Count how many components we're waiting for
	componentsToWait := 3

	// Wait for components to signal they're done or timeout
	for componentsToWait > 0 {
		select {
		case <-doneCh:
			componentsToWait--
			GetAppLogger().Println("Shutdown", fmt.Sprintf("Component shutdown acknowledged, %d remaining", componentsToWait))
		case <-timeout.C:
			GetAppLogger().Println("Shutdown", "Timeout waiting for components to shut down")
			return
		}
	}

	// Clean up resources
	isRun = false
	readyHealthCheck = false

	// Shutdown secure communication
	//secure.StopNet()

	GetAppLogger().Println("Shutdown", "All components shut down successfully")
}

// Debug mode functions

// promaxLogMessage logs a message to both the file logger and stdout
func promaxLogMessage(logger *logfile.FileLogger, tag string, message string) {
	logger.Println(tag, message)
	if config.IsDebugEnabled() {
		fmt.Printf("[%s] %s\n", tag, message)
	}
}

// process creates and sends metrics and help information
func process(logger *logfile.FileLogger) {
	// Create metrics
	metrics := createMetrics()
	promaxLogMessage(logger, "PromaX", fmt.Sprintf("Created %d metrics", len(metrics)))

	// Create help information if needed
	helpItems := make([]*model.OpenMxHelp, 0)
	now := time.Now().UnixMilli()

	// Send help information only once per minute
	if now-lastHelpSendTime > 60*dateutil.MILLIS_PER_SECOND {
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

		// Get the security master
		promaxLogMessage(logger, "PromaX", fmt.Sprintf("Sending %d help records", len(helpItems)))
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
	metrics := make([]*model.OpenMx, 0, 100)

	// Add metrics with no labels
	promaxAddNoLabelMetrics(&metrics)

	// Add metrics with one label
	promaxAddOneLabelsMetrics(&metrics)

	// Add metrics with two labels
	promaxAddTwoLabelsMetrics(&metrics)

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

// Helper function to split a label string in the format "key=value" into key and value
func promaxSplitLabel(label string) []string {
	idx := strings.Index(label, "=")
	if idx == -1 {
		return []string{}
	}
	return []string{label[:idx], label[idx+1:]}
}
