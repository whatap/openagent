package open

import (
	"fmt"
	"github.com/whatap/gointernal/net/secure"
	"github.com/whatap/golib/config/conffile"
	"github.com/whatap/golib/logger/logfile"
	"github.com/whatap/golib/util/dateutil"
	"open-agent/pkg/config"
	"open-agent/pkg/model"
	"open-agent/pkg/processor"
	"open-agent/pkg/scraper"
	"open-agent/pkg/sender"
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

	GetAppLogger().Println("BootOpenAgent", fmt.Sprintf("Starting OpenAgent version=%s, commitHash=%s", version, commitHash))

 // Read configuration
	conf := conffile.GetConfig()

	accessKey := conf.GetValue("accesskey")
	serverList := conf.GetValue("whatap.server.host")

	if accessKey == "" || serverList == "" {
		msg := fmt.Sprintf("Config Error - accessKey: %s, whatap.server.host: %s", accessKey, serverList)
		GetAppLogger().Println("BootOpenAgent", msg)
		return
	}

	// Parse server list
	servers := make([]string, 0)
	serverSlice := strings.Split(serverList, "/")
	port := conf.GetInt("net_udp_port", 6600)

	for _, str := range serverSlice {
		servers = append(servers, fmt.Sprintf("%s:%d", str, port))
	}

	// Initialize secure communication
	secure.StartNet(secure.WithLogger(logger), secure.WithAccessKey(accessKey), secure.WithServers(servers))

	// Create channels for communication between components
	rawQueue := make(chan *model.ScrapeRawData, RawQueueSize)
	processedQueue := make(chan *model.ConversionResult, ProcessedQueueSize)

	// Create the configuration manager
	configManager := config.NewConfigManager()

	// Create and start the scraper manager with error recovery and shutdown handling
	scraperManager := scraper.NewScraperManager(configManager, rawQueue)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Println("ScraperManagerPanic", fmt.Sprintf("Recovered from panic: %v", r))
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

	// Create and start the processor with error recovery and shutdown handling
	processor := processor.NewProcessor(rawQueue, processedQueue)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Println("ProcessorPanic", fmt.Sprintf("Recovered from panic: %v", r))
				// Restart the processor after a short delay
				select {
				case <-shutdownCh:
					// Don't restart if we're shutting down
					doneCh <- struct{}{}
					return
				case <-time.After(5 * time.Second):
					processor.Start()
				}
			} else {
				// Normal exit
				doneCh <- struct{}{}
			}
		}()

		// Start processing in a separate goroutine so we can listen for shutdown
		processDone := make(chan struct{})
		go func() {
			processor.Start()
			close(processDone)
		}()

		// Wait for either processing to finish or shutdown signal
		select {
		case <-processDone:
			// Processing finished normally
		case <-shutdownCh:
			// Shutdown requested, cleanup will be handled by defer
			logger.Println("Processor", "Shutdown requested")
			// Here we would call a stop method on processor if it had one
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
			logger.Println("Sender", "Shutdown requested")
			// Call Stop on the sender
			senderInstance.Stop()
		}
	}()

	// Set flags to indicate the agent is running
	isRun = true
	runDate = dateutil.SystemNow()

	logger.Println("BootOpenAgent", "OpenAgent started successfully")
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
