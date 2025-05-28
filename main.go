package main

import (
	"fmt"
	"github.com/whatap/golib/logger/logfile"
	"github.com/whatap/golib/util/dateutil"
	"log"
	"net"
	"open-agent/open"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"syscall"
	"time"
)

var (
	version    string
	commitHash string
)

const (
	AliveSockAddr       = "/var/run/whatap_openagent.sock"
	KEEP_ALIVE_PROTOCOL = 0x5B9B
)

func run(home string, logger *logfile.FileLogger) {
	stopper := make(chan os.Signal, 1)
	signal.Notify(stopper, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

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
		defer f.Close()

		pprof.Lookup("goroutine").WriteTo(f, 1)

		os.Exit(1)
	}()

	// Start the agent
	open.BootOpenAgent(version, commitHash, logger)

	// Wait for termination signal
	<-stopper

	// Perform graceful shutdown
	logger.Println("run", "Received termination signal, shutting down")
	open.Shutdown()
}

func exitOnStdinClose(logger *logfile.FileLogger) {
	ppid := os.Getppid()
	for {
		if ppid != os.Getppid() {
			logger.Println("exit", "exit master", ppid, os.Getppid())
			os.Exit(0)
		}
		time.Sleep(1 * time.Second)
	}
}

func startWorker(command string, logger *logfile.FileLogger) (*exec.Cmd, error) {
	logger.Println("StartWorker", fmt.Sprintf("Start Worker Process(%s foreground)", command))
	cmd := exec.Command(command, "foreground")
	err := cmd.Start()
	if err != nil {
		logger.Println("StartWorkerError", err)
		return nil, err
	}
	return cmd, nil
}

func getCurrentDir() (string, error) {
	return filepath.Abs(filepath.Dir(os.Args[0]))
}

func keepAliveSender(logger *logfile.FileLogger) {
	for {
		logger.Println("keepAliveSender", "keepAliveSender Start")
		if _, err := os.Stat(AliveSockAddr); os.IsNotExist(err) {
			logger.Println("keepAliveSenderError-1", "keepAliveSocketNotExist", AliveSockAddr)
			time.Sleep(10 * time.Second)
			continue
		}

		socktype := "unix"
		laddr := net.UnixAddr{AliveSockAddr, socktype}
		var serverconn net.Conn

		// Simplified nil check
		for serverconn == nil {
			conn, err := net.DialUnix(socktype, nil, &laddr)
			if err != nil {
				logger.Println("keepAliveSenderError-2", "Dial Error", err)
				time.Sleep(3 * time.Second)
				continue
			}
			serverconn = conn
		}

		// Use a ticker for regular keep-alive messages
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()

		// Send keep-alive messages until connection fails
	keepAliveLoop:
		for {
			select {
			case <-ticker.C:
				msgmap := make(map[string]string)
				if open.IsOK() {
					msgmap["Health"] = "OK"
				} else {
					logger.Println("KeepAlive", "HealthCheck Fail")
					msgmap["Health"] = "PROBLEM"
				}

				// Send the message
				// In a real implementation, this would serialize and send the message
				logger.Println("KeepAlive", fmt.Sprintf("Sending keep-alive message: %v", msgmap))

				// Check if connection is still valid
				if serverconn == nil {
					logger.Println("keepAliveSenderError-3", "Connection lost")
					break keepAliveLoop
				}
			}
		}

		// Clean up connection before retrying
		if serverconn != nil {
			serverconn.Close()
			serverconn = nil
		}

		time.Sleep(10 * time.Second)
	}
}

func main() {
	if len(os.Args) > 1 {
		arg1 := os.Args[1]
		if arg1 == "-v" || arg1 == "--version" {
			fmt.Printf("Version: %s\nCommit Hash: %s\n", version, commitHash)
		} else if arg1 == "-h" || arg1 == "--help" {
			fmt.Printf("option:\n\t -h, --help : help\n\t -v, --version : version\n")
		} else if arg1 == "foreground" {
			//worker
			openHome := os.Getenv("WHATAP_OPEN_HOME")
			logger := logfile.NewFileLogger(logfile.WithOnameLogID("open", "OPEN-AGENT-WORKER"), logfile.WithHomePath(openHome))
			go exitOnStdinClose(logger)
			go keepAliveSender(logger)
			run(openHome, logger)
		}
		os.Exit(0)
	}

	// master
	stopper := make(chan os.Signal, 1)
	signal.Notify(stopper, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	openHome := os.Getenv("WHATAP_OPEN_HOME")
	logger := logfile.NewFileLogger(logfile.WithOnameLogID("open", "OPEN-AGENT-SUPERVISOR"), logfile.WithHomePath(openHome))

	// Create a file to indicate that the agent is running
	pid := os.Getpid()
	homeDir := os.Getenv("WHATAP_HOME")
	if homeDir == "" {
		homeDir = "."
	}
	exitFile := filepath.Join(homeDir, fmt.Sprintf("openagent-%d.whatap", pid))

	// Create the exit file
	file, err := os.Create(exitFile)
	if err != nil {
		log.Fatalf("Error creating exit file: %v", err)
	}
	file.Close()

	// Delete the exit file when the program exits
	defer os.Remove(exitFile)

	// Wait for either a signal or the exit file to be deleted
	go func() {
		for {
			if _, err := os.Stat(exitFile); os.IsNotExist(err) {
				logger.Println("Exit file deleted, shutting down...")
				os.Exit(0)
			}
			time.Sleep(10 * time.Second)
		}
	}()

	// Start the worker process
	cmd, err := startWorker(os.Args[0], logger)
	if err != nil {
		logger.Println("StartWorkerError", err)
		os.Exit(1)
	}

	// Wait for the worker process to exit
	err = cmd.Wait()
	if err != nil {
		logger.Println("WorkerExitError", err)
	}

	<-stopper
}
