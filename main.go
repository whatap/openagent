package main

// OpenAgent - A Prometheus metrics collector for WHATAP
//
// This application collects metrics from Prometheus endpoints and sends them to the WHATAP server.
// It can run in two modes:
// 1. Supervisor mode (default): Manages a worker process and monitors its health
// 2. Worker mode (with "foreground" argument): Performs the actual metrics collection and sending

import (
	"fmt"
	"github.com/whatap/golib/logger/logfile"
	"github.com/whatap/golib/util/dateutil"
	"log"
	"net/http"
	_ "net/http/pprof"
	"open-agent/open"
	"open-agent/pkg/config"
	"os"
	"os/signal"
	"runtime"
	"runtime/pprof"
	"strconv"
	"syscall"
	"time"
)

var (
	version    string // Version of the application
	commitHash string // Git commit hash
)

// startPprofServer starts the pprof HTTP server for performance profiling
func startPprofServer(logger *logfile.FileLogger) {
	// Get pprof port from environment variable, default to 6060
	pprofPortStr := os.Getenv("PPROF_PORT")
	if pprofPortStr == "" {
		pprofPortStr = "6060"
	}

	pprofPort, err := strconv.Atoi(pprofPortStr)
	if err != nil {
		logger.Infoln("pprof", "Invalid PPROF_PORT value, using default 6060")
		pprofPort = 6060
	}

	pprofAddr := fmt.Sprintf(":%d", pprofPort)

	go func() {
		logger.Infoln("pprof", fmt.Sprintf("Starting pprof server on %s", pprofAddr))
		logger.Infoln("pprof", "Available endpoints:")
		logger.Infoln("pprof", fmt.Sprintf("  - CPU Profile: http://localhost%s/debug/pprof/profile", pprofAddr))
		logger.Infoln("pprof", fmt.Sprintf("  - Heap Profile: http://localhost%s/debug/pprof/heap", pprofAddr))
		logger.Infoln("pprof", fmt.Sprintf("  - Goroutine Profile: http://localhost%s/debug/pprof/goroutine", pprofAddr))
		logger.Infoln("pprof", fmt.Sprintf("  - All Profiles: http://localhost%s/debug/pprof/", pprofAddr))

		if err := http.ListenAndServe(pprofAddr, nil); err != nil {
			logger.Infoln("pprof", fmt.Sprintf("Failed to start pprof server: %v", err))
		}
	}()
}

func run(home string, logger *logfile.FileLogger) {
	// Set up signal handling for graceful shutdown
	stopper := make(chan os.Signal, 1)
	signal.Notify(stopper, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	// Start pprof HTTP server for performance profiling
	startPprofServer(logger)

	// Enable CPU profiling
	runtime.SetCPUProfileRate(1)

	// Set up signal handling for crash dumps
	dump := make(chan os.Signal, 1)
	signal.Notify(dump, syscall.SIGSEGV, syscall.SIGABRT)
	go func() {
		<-dump
		// Create stack dump

		if home == "" {
			home = os.Getenv("WHATAP_HOME")
			if home == "" {
				home = "./"
			}
		}

		stackFile := fmt.Sprintf("%s/logs/stack-%s.dump", home, dateutil.YYYYMMDD(dateutil.Now()))
		f, err := os.Create(stackFile)
		if err != nil {
			log.Fatal(err)
		}
		defer func(f *os.File) {
			err := f.Close()
			if err != nil {
				logger.Infoln("run", "Error closing stack dump file", err)
			}
		}(f)

		err = pprof.Lookup("goroutine").WriteTo(f, 1)
		if err != nil {
			logger.Infoln("run", "Error writing stack dump file", err)
			return
		}

		os.Exit(1)
	}()

	// Start the agent
	open.BootOpenAgent(version, commitHash, logger)

	logger.Infoln("run", "Received termination signal, shutting down")
	<-stopper
}

func exitOnStdinClose(logger *logfile.FileLogger) {
	ppid := os.Getppid()
	for {
		if ppid != os.Getppid() {
			logger.Infoln("exit", "exit master", ppid, os.Getppid())
			os.Exit(0)
		}
		time.Sleep(1 * time.Second)
	}
}

func main() {

	printWhatap := fmt.Sprint("\n" +
		" _      ____       ______WHATAP-OPEN-AGENT\n" +
		"| | /| / / /  ___ /_  __/__ ____\n" +
		"| |/ |/ / _ \\/ _ `// / / _ `/ _ \\\n" +
		"|__/|__/_//_/\\_,_//_/  \\_,_/ .__/\n" +
		"                          /_/\n" +
		"Just Tap, Always Monitoring\n")
	fmt.Println(printWhatap)

	// Check if version is set from environment variable
	if len(os.Args) > 1 {
		arg1 := os.Args[1]
		if arg1 == "foreground" {
			if config.IsDebugEnabled() {
				fmt.Println("mode:foreground")
			}

			// Check if we have a second argument
			if len(os.Args) > 2 {
				arg2 := os.Args[2]
				if arg2 == "standalone" {
					if config.IsDebugEnabled() {
						fmt.Println("Standalone configuration mode: enabled")
					}
					// Force standalone configuration mode
					config.SetForceStandaloneMode(true)
				}
			}

			//worker
			openHome := os.Getenv("WHATAP_OPEN_HOME")
			logger := logfile.NewFileLogger(logfile.WithOnameLogID("open", "OPEN-AGENT"), logfile.WithHomePath(openHome))
			go exitOnStdinClose(logger)
			run(openHome, logger)
		} else if arg1 == "standalone" {
			if config.IsDebugEnabled() {
				fmt.Println("Standalone mode: enabled")
			}
			config.SetForceStandaloneMode(true)
		}
	}

	if config.IsDebugEnabled() {
		fmt.Println("mode:default (worker)")
	}

	openHome := os.Getenv("WHATAP_OPEN_HOME")
	logger := logfile.NewFileLogger(logfile.WithOnameLogID("open", "OPEN-AGENT"), logfile.WithHomePath(openHome))

	run(openHome, logger)
}
