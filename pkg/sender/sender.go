package sender

import (
	"fmt"
	"sync"
	"time"

	"github.com/whatap/gointernal/net/secure"
	"github.com/whatap/golib/lang/pack"
	"github.com/whatap/golib/logger/logfile"

	"open-agent/pkg/model"
)

const (
	// ChunkSize is the maximum number of metrics to send in a single batch
	ChunkSize = 1000

	// MaxRetries is the maximum number of retries for sending data
	MaxRetries = 3

	// RetryDelay is the delay between retries
	RetryDelay = 5 * time.Second
)

// Sender is responsible for sending processed metrics to the server
type Sender struct {
	processedQueue chan *model.ConversionResult
	logger         *logfile.FileLogger
	shutdownCh     chan struct{}
	doneCh         chan struct{}
	lastSendTime   map[string]int64
	mu             sync.Mutex
}

// NewSender creates a new Sender instance
func NewSender(processedQueue chan *model.ConversionResult, logger *logfile.FileLogger) *Sender {
	if logger == nil {
		// Fallback to a default logger if not provided
		logger = logfile.NewFileLogger()
	}

	return &Sender{
		processedQueue: processedQueue,
		logger:         logger,
		shutdownCh:     make(chan struct{}),
		doneCh:         make(chan struct{}),
		lastSendTime:   make(map[string]int64),
	}
}

// Start starts the sender
func (s *Sender) Start() {
	// The logger should already be set in the constructor
	// If it's not set for some reason, create a default one
	if s.logger == nil {
		s.logger = logfile.NewFileLogger()
	}

	go s.sendLoop()
}

// Stop gracefully stops the sender
func (s *Sender) Stop() {
	close(s.shutdownCh)
	<-s.doneCh
}

// sendLoop continuously sends processed data from the queue
func (s *Sender) sendLoop() {
	defer func() {
		if r := recover(); r != nil {
			s.logger.Println("SenderPanic", fmt.Sprintf("Recovered from panic: %v", r))
		}
		close(s.doneCh)
	}()

	for {
		select {
		case <-s.shutdownCh:
			s.logger.Println("Sender", "Shutdown requested, exiting send loop")
			return
		case result, ok := <-s.processedQueue:
			if !ok {
				s.logger.Println("Sender", "Process queue closed, exiting send loop")
				return
			}
			s.sendResult(result)
		}
	}
}

// sendResult sends a single conversion result
func (s *Sender) sendResult(result *model.ConversionResult) {
	// Log target and timestamp information
	if result.GetTarget() != "" {
		collectionTime := time.UnixMilli(result.GetCollectionTime())
		s.logger.Println("Sender", fmt.Sprintf("Processing data for target: %s, collected at: %s",
			result.GetTarget(), collectionTime.Format(time.RFC3339)))

		// Check for duplicate metrics (same target, same timestamp)
		s.mu.Lock()
		if lastTime, exists := s.lastSendTime[result.GetTarget()]; exists {
			if lastTime == result.GetCollectionTime() {
				s.logger.Println("Sender", fmt.Sprintf("WARNING: Duplicate metrics detected for target %s at time %d (%s). This may cause data duplication.",
					result.GetTarget(), result.GetCollectionTime(), collectionTime.Format(time.RFC3339)))
			}
		}
		s.lastSendTime[result.GetTarget()] = result.GetCollectionTime()
		s.mu.Unlock()
	}

	// Send OpenMxHelp data
	openMxHelpList := result.GetOpenMxHelpList()
	if len(openMxHelpList) > 0 {
		s.sendHelp(openMxHelpList)
	}

	// Send OpenMx data
	openMxList := result.GetOpenMxList()
	if len(openMxList) > 0 {
		s.sendMetrics(openMxList)
	}
}

// sendHelp sends OpenMxHelp data in chunks
func (s *Sender) sendHelp(helpList []*model.OpenMxHelp) {
	total := len(helpList)
	for i := 0; i < total; i += ChunkSize {
		end := i + ChunkSize
		if end > total {
			end = total
		}
		chunk := helpList[i:end]

		s.logger.Println("Sender", fmt.Sprintf("Sending %d OpenMxHelp records", len(chunk)))

		// Create a pack and send it
		helpPack := createHelpPack(chunk)
		s.sendToServerWithRetry(helpPack)
	}
}

// sendMetrics sends OpenMx data in chunks
func (s *Sender) sendMetrics(metrics []*model.OpenMx) {
	total := len(metrics)
	for i := 0; i < total; i += ChunkSize {
		end := i + ChunkSize
		if end > total {
			end = total
		}
		chunk := metrics[i:end]

		s.logger.Println("Sender", fmt.Sprintf("Sending %d OpenMx records", len(chunk)))

		// Create a pack and send it
		metricsPack := createMetricsPack(chunk)
		s.sendToServerWithRetry(metricsPack)
	}
}

// createHelpPack creates a pack of OpenMxHelp records for sending
func createHelpPack(helpList []*model.OpenMxHelp) pack.Pack {
	// Create a pack for the help data
	p := model.NewOpenMxHelpPack()
	p.SetRecords(helpList)

	return p
}

// createMetricsPack creates a pack of OpenMx records for sending
func createMetricsPack(metrics []*model.OpenMx) pack.Pack {
	// Create a pack for the metrics data
	p := model.NewOpenMxPack()
	p.SetRecords(metrics)

	return p
}

// sendToServerWithRetry sends a pack to the server with retry logic
func (s *Sender) sendToServerWithRetry(p pack.Pack) {
	var err error

	for retry := 0; retry < MaxRetries; retry++ {
		if retry > 0 {
			s.logger.Println("SenderRetry", fmt.Sprintf("Retrying send (attempt %d/%d)", retry+1, MaxRetries))
			time.Sleep(RetryDelay)
		}

		err = s.sendToServer(p)
		if err == nil {
			return
		}

		s.logger.Println("SenderError", fmt.Sprintf("Error sending data: %v", err))
	}

	s.logger.Println("SenderFailed", fmt.Sprintf("Failed to send data after %d attempts", MaxRetries))
}

// sendToServer sends a pack to the server
func (s *Sender) sendToServer(p pack.Pack) error {
	// Get the security master from the secure package
	securityMaster := secure.GetSecurityMaster()
	if securityMaster == nil {
		return fmt.Errorf("no security master available")
	}

	// Set the PCODE and OID from the security master
	p.SetPCODE(securityMaster.PCODE)
	p.SetOID(securityMaster.OID)

	// Set the time to the current time
	p.SetTime(time.Now().UnixMilli())

	// Send the pack to the server using secure.Send
	secure.Send(secure.NET_SECURE_HIDE, p, true)

	return nil
}
