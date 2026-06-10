package open

import (
	"context"
	"fmt"
	"math/rand"
	"open-agent/pkg/config"
	"open-agent/pkg/control"
	"open-agent/pkg/counter"
	"open-agent/pkg/discovery"
	"open-agent/pkg/k8s"
	"open-agent/pkg/model"
	"open-agent/pkg/processor"
	"open-agent/pkg/scraper"
	"open-agent/pkg/sender"
	"open-agent/tools/util/logutil"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/whatap/gointernal/net/secure"
	golibconfig "github.com/whatap/golib/config"
	"github.com/whatap/golib/logger/logfile"
	"github.com/whatap/golib/util/dateutil"
)

const (
	// QueueSize is the size of the queues
	RawQueueSize       = 10000
	ProcessedQueueSize = 10000
)

// appLogger is a process-wide logger reference set when an Agent is
// constructed. External callers obtain it via GetAppLogger.
var appLogger *logfile.FileLogger

// Test-mode globals, used only by the optional `test=true` env loop in Run.
// Kept at package scope because the test loop predates the Agent refactor
// and there is no benefit to making it reentrant.
var (
	deltaMap         = make(map[string]int64)
	lastHelpSendTime int64
)

// defaultAgent backs the package-level BootOpenAgent / Shutdown / IsOK
// helpers. New code should construct an Agent directly via NewAgent.
var (
	defaultAgentMu sync.Mutex
	defaultAgent   *Agent
)

// SetAppLogger sets the process-wide logger reference.
func SetAppLogger(logger *logfile.FileLogger) {
	appLogger = logger
}

// GetAppLogger returns the process-wide logger reference.
func GetAppLogger() *logfile.FileLogger {
	return appLogger
}

// Agent is one runnable instance of the OpenAgent worker pipeline.
//
// An Agent can be Run and Shutdown repeatedly within a single process, which
// is required for HA modes that gate scraping behind leader election (Run on
// leader acquire, Shutdown on leader loss).
type Agent struct {
	version    string
	commitHash string
	buildTime  string
	logger     *logfile.FileLogger

	mu               sync.Mutex
	isRun            bool
	readyHealthCheck bool
	runDate          int64

	// cancel cancels the context derived in Run. Shutdown calls it to
	// signal background goroutines (notably ServiceDiscovery) to exit.
	// nil when the agent is not running.
	cancel context.CancelFunc

	// wg tracks goroutines owned directly by Run that do not have their
	// own Stop() (currently: the ServiceDiscovery launcher). Stored as a
	// pointer so each Run can swap in a fresh WaitGroup without racing
	// against late wg.Done() calls from a previous Run's goroutines (each
	// goroutine captures the pointer locally before calling Done).
	wg *sync.WaitGroup

	// Component references retained so that Shutdown can stop them in the
	// proper data-flow-reverse order. Cleared on Shutdown.
	scraperManager   *scraper.ScraperManager
	processor        *processor.Processor
	senderInstance   *sender.Sender
	serviceDiscovery discovery.ServiceDiscovery
}

// NewAgent constructs an Agent. It does not start any goroutines or open
// any network connections; call Run to start the pipeline.
func NewAgent(version, commitHash, buildTime string, logger *logfile.FileLogger) *Agent {
	if version == "" {
		version = "dev"
	}
	if commitHash == "" {
		commitHash = "unknown"
	}
	return &Agent{
		version:    version,
		commitHash: commitHash,
		buildTime:  buildTime,
		logger:     logger,
	}
}

// Run starts the agent pipeline (Scraper, Processor, Sender). For normal
// (non-test) modes Run returns once initialization completes; the actual
// pipeline runs in goroutines until Shutdown is called. In test mode
// (env test=true) Run blocks forever in the legacy test loop.
//
// Calling Run on an already-running Agent is a no-op.
//
// The context is currently used for ServiceDiscovery. Full context-based
// shutdown of Scraper/Processor/Sender is a follow-up.
func (a *Agent) Run(ctx context.Context) {
	a.mu.Lock()
	if a.isRun {
		a.mu.Unlock()
		if a.logger != nil {
			a.logger.Infoln("Agent.Run", "Agent already running")
		}
		return
	}
	// Derive a per-run cancellable context. Shutdown calls cancel to
	// signal background goroutines. Allocate a fresh WaitGroup pointer
	// so that any leftover Done() calls from a previous Run (e.g. after
	// a Shutdown timeout) operate on the old WaitGroup rather than this
	// one. Goroutines below capture wg locally to preserve that contract.
	derivedCtx, cancel := context.WithCancel(ctx)
	a.cancel = cancel
	a.wg = &sync.WaitGroup{}
	wg := a.wg
	a.mu.Unlock()
	ctx = derivedCtx

	// Store the logger in the package-level reference for centralized access.
	SetAppLogger(a.logger)
	logger := a.logger

	logutil.Printf("START", "\nWHATAP Open Agent Starting\n")
	logutil.Printf("START", " Version: %s\n", a.version)
	logutil.Printf("START", " Build: %s\n", a.commitHash)
	logutil.Printf("START", " Started at: %s\n\n", time.Now().Format("2006-01-02 15:04:05 MST"))

	// Get configuration values using the config package
	// Support multiple key formats for whatap.conf and environment variables
	servers := make([]string, 0)
	license := getFirstNonEmpty(
		config.Get("WHATAP_LICENSE"), // whatap.conf: WHATAP_LICENSE / env: WHATAP_LICENSE
		config.Get("license"),        // whatap.conf: license
	)
	hosts := getFirstNonEmpty(
		config.Get("WHATAP_HOST"),        // whatap.conf: WHATAP_HOST / env: WHATAP_HOST
		config.Get("whatap.server.host"), // whatap.conf: whatap.server.host
		os.Getenv("WHATAP_SERVER_HOST"),  // env: WHATAP_SERVER_HOST
	)
	port := getFirstNonZeroInt(
		config.GetIntWithDefault("WHATAP_PORT", 0),        // whatap.conf: WHATAP_PORT / env: WHATAP_PORT
		config.GetIntWithDefault("whatap.server.port", 0), // whatap.conf: whatap.server.port
		getEnvInt("WHATAP_SERVER_PORT", 0),                // env: WHATAP_SERVER_PORT
	)
	if port == 0 {
		port = 6600
	}
	if license == "" || hosts == "" {
		logutil.Println("SETTING", "Please set the following configuration values:")
		logutil.Println("SETTING", "  license: WHATAP_LICENSE (conf/env) or license (conf)")
		logutil.Println("SETTING", "  host:    WHATAP_HOST (conf/env), whatap.server.host (conf), or WHATAP_SERVER_HOST (env)")
		logutil.Println("SETTING", "  port:    WHATAP_PORT (conf/env), whatap.server.port (conf), or WHATAP_SERVER_PORT (env) (default: 6600)")
		os.Exit(1)
	}

	hostSlice := strings.FieldsFunc(hosts, func(r rune) bool {
		return r == '/' || r == ','
	})
	// Parse server list
	for _, hostSliced := range hostSlice {
		if hostTrimmed := strings.TrimSpace(hostSliced); len(hostTrimmed) > 0 {
			servers = append(servers, fmt.Sprintf("%s:%d", hostTrimmed, port))
		}
	}

	// Set logger level based on log_level configuration or debug configuration
	configLogLevel := config.Get("log_level")
	var logLevel int = -1

	if configLogLevel != "" {
		// Try to parse as integer first
		if level, err := strconv.Atoi(configLogLevel); err == nil {
			logLevel = level
		} else {
			// Try to parse as string
			switch strings.ToUpper(configLogLevel) {
			case "DEBUG":
				logLevel = logutil.LOG_LEVEL_DEBUG
			case "INFO":
				logLevel = logutil.LOG_LEVEL_INFO
			case "WARN", "WARNING":
				logLevel = logutil.LOG_LEVEL_WARN
			case "ERROR":
				logLevel = logutil.LOG_LEVEL_ERROR
			}
		}
	}

	if logLevel != -1 {
		logutil.SetLevel(logLevel)
		logutil.Infof("CONFIG", "Log level set to %d (%s) from log_level config", logLevel, configLogLevel)
	} else {
		// Set logger level based on debug configuration from whatap.conf
		if config.IsDebugEnabled() {
			logutil.SetLevel(logutil.LOG_LEVEL_DEBUG) // LOG_LEVEL_DEBUG = 0
			logutil.Infof("CONFIG", "Debug logging enabled from whatap.conf")
		} else {
			logutil.SetLevel(logutil.LOG_LEVEL_INFO) // LOG_LEVEL_INFO = 1
			logutil.Infof("CONFIG", "Debug logging disabled from whatap.conf")
		}
	}

	// Register FileLogger with ConfigObserver so log_level from whatap.conf is applied
	golibconfig.GetConfigObserver().Add("FileLogger", logger)

	// Determine oname: whatap.oname > WHATAP_ONAME > app_name (all used directly, no pattern)
	oname := config.Get("whatap.oname")
	if oname == "" {
		oname = os.Getenv("WHATAP_ONAME")
	}
	if oname == "" {
		oname = config.Get("app_name")
	}
	if oname != "" {
		logutil.Infof("CONFIG", "oname: %s", oname)
	} else {
		logutil.Infof("CONFIG", "No oname set (whatap.oname / WHATAP_ONAME / app_name), will use auto-generated pattern")
	}

	// Determine object_name pattern: whatap.name > WHATAP_NAME > object_name (for auto-generation when oname is empty)
	objectNamePattern := config.Get("whatap.name")
	if objectNamePattern == "" {
		objectNamePattern = os.Getenv("WHATAP_NAME")
	}
	if objectNamePattern == "" {
		objectNamePattern = config.Get("object_name")
	}

	// Determine okind: whatap.okind > WHATAP_OKIND
	okindName := config.Get("whatap.okind")
	if okindName == "" {
		okindName = os.Getenv("WHATAP_OKIND")
	}

	// Determine onode: whatap.onode > WHATAP_ONODE
	onodeName := config.Get("whatap.onode")
	if onodeName == "" {
		onodeName = os.Getenv("WHATAP_ONODE")
	}

	// Initialize secure communication
	opts := []secure.TcpSessionOption{
		secure.WithLogger(logger),
		secure.WithAccessKey(license),
		secure.WithServers(servers),
		secure.WithOname(oname),
		secure.WithOkindName(okindName),
		secure.WithOnodeName(onodeName),
		secure.WithConfigObserver(golibconfig.GetConfigObserver()),
	}
	if objectNamePattern != "" {
		opts = append(opts, secure.WithObjectName(objectNamePattern))
		logutil.Infof("CONFIG", "object_name pattern: %s", objectNamePattern)
	}
	secure.StartNet(opts...)

	// Apply initial config from whatap.conf to secure package
	golibconfig.GetConfigObserver().Run(config.GetInstance())

	// Apply log_keep_days config to logutil.Logger (golib FileLogger reads it via ApplyConfig automatically)
	conf := config.GetConfig()
	logutil.SetLogKeepDays(conf.LogKeepDays)

	// Start control handler for server-side commands (GET_ENV, CONFIGURE_GET, SET_CONFIG, AGENT_LOG_LIST, AGENT_LOG_READ)
	control.InitControlHandler(logger)

	// Read config flags
	tagCounterEnabled := config.GetBoolWithDefault("tag_counter_enabled", false)
	endpointMeteringEnabled := config.GetBoolWithDefault("endpoint_metering_enabled", false)
	logutil.Infof("CONFIG", "tag_counter_enabled=%v, endpoint_metering_enabled=%v", tagCounterEnabled, endpointMeteringEnabled)

	// Start CounterManager if either tag_counter or endpoint_metering is enabled
	if tagCounterEnabled || endpointMeteringEnabled {
		counter.StartCounterManager(tagCounterEnabled, endpointMeteringEnabled)
	} else {
		logutil.Infof("CONFIG", "CounterManager disabled")
	}

	// Check if test mode is enabled
	testMode := os.Getenv("test")
	if testMode == "true" {
		logutil.Infoln("BootOpenAgent", "test mode enabled, running process method")
		// Initialize random number generator
		rand.Seed(time.Now().UnixNano())
		// Run the process method in a loop
		for {
			process(logger)
			time.Sleep(10 * time.Second)
		}
		// The code below will not be executed in test mode
	}

	logutil.Infoln("BootOpenAgent, test-mode disabled")
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
	serviceDiscovery := discovery.NewServiceDiscovery(configManager)
	a.serviceDiscovery = serviceDiscovery
	// Start service discovery as an independent component. Tracked by the
	// per-run WaitGroup (captured locally so a future Run cannot reassign
	// it under us) so Shutdown can wait for the launcher to finish.
	wg.Add(1)
	go func() {
		defer wg.Done()
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
			if err := serviceDiscovery.Start(ctx); err != nil {
				logutil.Infoln("ServiceDiscovery", fmt.Sprintf("Failed to start service discovery: %v", err))
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

	// Each component below spawns its own internal goroutine on Start,
	// handles its own panic recovery, and exits cleanly on Stop. We hold
	// references on the Agent so Shutdown can drive their lifecycle in
	// the proper data-flow-reverse order (Scraper -> Processor -> Sender).
	a.scraperManager = scraperManager
	scraperManager.StartScraping()

	newProcessor := processor.NewProcessor(rawQueue, processedQueue)
	a.processor = newProcessor
	newProcessor.Start()

	a.senderInstance = sender.NewSender(processedQueue, GetAppLogger(), endpointMeteringEnabled)
	a.senderInstance.Start()

	// Set flags to indicate the agent is running
	a.mu.Lock()
	a.isRun = true
	a.runDate = dateutil.SystemNow()
	a.mu.Unlock()

	logger.Infoln("BootOpenAgent", "OpenAgent started successfully")
}

// IsOK reports whether the agent is running and the secure session is healthy.
func (a *Agent) IsOK() bool {
	a.mu.Lock()
	isRun := a.isRun
	runDate := a.runDate
	ready := a.readyHealthCheck
	a.mu.Unlock()

	// If health check is not ready yet, check if it's time to enable it
	if !ready {
		if isRun && (dateutil.SystemNow()-runDate > 2*dateutil.MILLIS_PER_MINUTE) {
			a.logger.Println("HealthCheckReady", "Worker HealthCheck Ready")
			a.mu.Lock()
			a.readyHealthCheck = true
			a.mu.Unlock()
		}
		return true // Return healthy until health check is ready
	}

	// Perform actual health check
	secu := secure.GetSecurityMaster()

	// Check PCODE
	if secu.PCODE == 0 {
		a.logger.Println("HealthCheckFail", fmt.Sprintf("PCODE Error: %d", secu.PCODE))
		return false
	}

	// Check OID
	if secu.OID == 0 {
		a.logger.Println("HealthCheckFail", fmt.Sprintf("OID Error: %d", secu.OID))
		return false
	}

	return true
}

// Shutdown gracefully stops the agent pipeline. Calling Shutdown on a
// non-running Agent (or concurrent calls) is a no-op: the isRun flag is
// flipped to false inside the same lock that gates the entry check, so at
// most one caller proceeds past it for any given Run cycle.
//
// Components are stopped in data-flow-reverse order so that each upstream
// stage drains into the next before the next stage stops accepting work:
// Scraper (no new scrapes) -> Processor (drain rawQueue) -> Sender (drain
// processedQueue) -> ServiceDiscovery (stop target refresh). The whole
// sequence is bounded by a 30-second timeout to guarantee Shutdown returns
// even if a component hangs.
func (a *Agent) Shutdown() {
	a.mu.Lock()
	if !a.isRun {
		a.mu.Unlock()
		if a.logger != nil {
			a.logger.Println("Shutdown", "Agent is not running")
		}
		return
	}
	// Flip state immediately inside the lock so a concurrent Shutdown
	// caller (e.g. SIGTERM handler racing with leader-election
	// OnStoppedLeading) returns at the check above and never reaches the
	// double-close paths in component Stop() implementations.
	a.isRun = false
	a.readyHealthCheck = false
	cancel := a.cancel
	sm := a.scraperManager
	p := a.processor
	s := a.senderInstance
	sd := a.serviceDiscovery
	wg := a.wg
	a.cancel = nil
	a.scraperManager = nil
	a.processor = nil
	a.senderInstance = nil
	a.serviceDiscovery = nil
	// Note: a.wg is intentionally not cleared here. Run allocates a fresh
	// pointer; leaving the field non-nil avoids a brief window where a
	// goroutine spawned by a hypothetical concurrent Run would observe nil.
	a.mu.Unlock()

	a.logger.Println("Shutdown", "Initiating graceful shutdown")

	// Cancel the per-run context so any ctx-aware code (e.g. ServiceDiscovery
	// goroutine if it ever consumes ctx) sees Done and exits.
	if cancel != nil {
		cancel()
	}

	done := make(chan struct{})
	go func() {
		if sm != nil {
			a.logger.Println("Shutdown", "Stopping scraper")
			sm.Stop()
		}
		if p != nil {
			a.logger.Println("Shutdown", "Stopping processor")
			p.Stop()
		}
		if s != nil {
			a.logger.Println("Shutdown", "Stopping sender")
			s.Stop()
		}
		if sd != nil {
			a.logger.Println("Shutdown", "Stopping discovery")
			if err := sd.Stop(); err != nil {
				a.logger.Println("Shutdown", fmt.Sprintf("Discovery stop error: %v", err))
			}
		}
		// Wait for goroutines owned directly by Run (the discovery
		// launcher). Use the locally captured wg pointer so this Wait is
		// pinned to the WaitGroup that Run added to, even if a future Run
		// has already swapped a.wg out.
		if wg != nil {
			wg.Wait()
		}
		close(done)
	}()

	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()

	select {
	case <-done:
		a.logger.Println("Shutdown", "All components shut down successfully")
	case <-timer.C:
		a.logger.Println("Shutdown", "Timeout waiting for components to shut down")
	}
}

// getOrCreateDefaultAgent returns the process-wide default Agent, creating
// it if necessary.
func getOrCreateDefaultAgent(version, commitHash, buildTime string, logger *logfile.FileLogger) *Agent {
	defaultAgentMu.Lock()
	defer defaultAgentMu.Unlock()
	if defaultAgent == nil {
		defaultAgent = NewAgent(version, commitHash, buildTime, logger)
	}
	return defaultAgent
}

// BootOpenAgent initializes and starts the Prometheus Agent on the
// process-wide default Agent.
//
// Retained for backward compatibility with main.go and existing callers.
// New code should construct its own Agent via NewAgent and call Run /
// Shutdown directly.
func BootOpenAgent(version, commitHash, buildTime string, logger *logfile.FileLogger) {
	a := getOrCreateDefaultAgent(version, commitHash, buildTime, logger)
	a.Run(context.Background())
}

// IsOK reports whether the default Agent is healthy.
func IsOK() bool {
	defaultAgentMu.Lock()
	a := defaultAgent
	defaultAgentMu.Unlock()
	if a == nil {
		return false
	}
	return a.IsOK()
}

// Shutdown gracefully shuts down the default Agent.
func Shutdown() {
	defaultAgentMu.Lock()
	a := defaultAgent
	defaultAgentMu.Unlock()
	if a == nil {
		return
	}
	a.Shutdown()
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

// getFirstNonEmpty returns the first non-empty string from the given values
func getFirstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// getFirstNonZeroInt returns the first non-zero int from the given values
func getFirstNonZeroInt(values ...int) int {
	for _, v := range values {
		if v != 0 {
			return v
		}
	}
	return 0
}

// getEnvInt reads an environment variable as int, returns def if not set or invalid
func getEnvInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}
