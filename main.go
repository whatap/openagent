package main

// OpenAgent - A Prometheus metrics collector for WHATAP
//
// This application collects metrics from Prometheus endpoints and sends them to the WHATAP server.
// It can run in two modes:
// 1. Supervisor mode (default): Manages a worker process and monitors its health
// 2. Worker mode (with "foreground" argument): Performs the actual metrics collection and sending

import (
	"fmt"
	whatap_io "github.com/whatap/golib/io"
	"github.com/whatap/golib/lang/value"
	"github.com/whatap/golib/logger/logfile"
	"github.com/whatap/golib/util/dateutil"
	"log"
	"net"
	"open-agent/open"
	"open-agent/pkg/client"
	"open-agent/pkg/k8s"
	"open-agent/util/io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"reflect"
	"runtime"
	"runtime/pprof"
	"syscall"
	"time"
)

var (
	version    string // Version of the application
	commitHash string // Git commit hash
)

// Constants for the keep-alive mechanism
const (
	KEEP_ALIVE_PROTOCOL = 0x5B9B // Protocol identifier for keep-alive messages
)

// getAliveSockAddr returns the socket address for keep-alive communication
// It uses the current working directory where main.go is executed
func getAliveSockAddr() string {
	// Get the current working directory
	cwd, err := os.Getwd()
	if err != nil {
		// If there's an error getting the current directory, fall back to temp directory
		return fmt.Sprintf("%s/whatap_openagent.sock", os.TempDir())
	}
	return fmt.Sprintf("%s/whatap_openagent.sock", cwd)
}

func run(home string, logger *logfile.FileLogger) {
	// Set up signal handling for graceful shutdown
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

	logger.Println("run", "Received termination signal, shutting down")
	<-stopper
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
func superviseRun(childHealthChannel chan bool, logger *logfile.FileLogger) {
	for {
		logger.Println("superviseRun", "Worker Start")
		cmd, err := startWorker(os.Args[0], logger)
		if err != nil {
			return
		}

		childAlive := true
		//childStart := time.Now()

		for childAlive {
			select {
			case healthStatus := <-childHealthChannel:
				if !healthStatus {
					if cmd != nil && cmd.Process != nil {
						cmd.Process.Signal(syscall.SIGABRT)
						err = cmd.Wait()
						childAlive = false
					}
					logger.Println("superviseCheck", "child health problem, child kill")
				}
			case <-time.After(time.Duration(3) * time.Minute):
				if cmd != nil && cmd.Process != nil {
					cmd.Process.Signal(syscall.SIGABRT)
					err = cmd.Wait()
					childAlive = false
				}
				logger.Println("superviseCheck", "child health check timeout, child kill")
			}
		}

		if err != nil {
			logger.Println("superviseRunError", "command wait error : ", err)
		}
	}
}
func sendKeepAliveMessage(logger *logfile.FileLogger, conn net.Conn) {
	writer := io.NewNetWriteHelper(conn)
	var err error
	for err == nil {
		msgmap := value.NewMapValue()
		if open.IsOK() {
			msgmap.PutString("Health", "OK")
		} else {
			logger.Println("KeepAlive", "HealthCheck Fail")
			msgmap.PutString("Health", "PROBLEM")
		}

		dout := whatap_io.NewDataOutputX()
		dout.WriteShort(KEEP_ALIVE_PROTOCOL)
		doutx := whatap_io.NewDataOutputX()
		msgmap.Write(doutx)
		dout.WriteIntBytes(doutx.ToByteArray())
		err = writer.WriteBytes(dout.ToByteArray(), 30*time.Second)
		if err != nil {
			logger.Println("KeepAliveWriteFail", "KeepAliveFail")
		}

		time.Sleep(60 * time.Second)
	}
}
func keepAliveSender(logger *logfile.FileLogger) {
	for {
		logger.Println("keepAliveSender", "keepAliveSender Start")
		sockAddr := getAliveSockAddr()
		if _, err := os.Stat(sockAddr); os.IsNotExist(err) {
			logger.Println("keepAliveSenderError-1", "keepAliveSocketNotExist", sockAddr)
			continue
		}
		socktype := "unix"
		laddr := net.UnixAddr{sockAddr, socktype}
		var serverconn net.Conn
		for serverconn == nil || (reflect.ValueOf(serverconn).Kind() == reflect.Ptr && reflect.ValueOf(serverconn).IsNil()) {
			conn, err := net.DialUnix(socktype, nil, &laddr)
			if err != nil {
				logger.Println("keepAliveSenderError-2", "Dial Error", err)
				time.Sleep(3 * time.Second)
				continue
			}
			serverconn = conn
		}

		sendKeepAliveMessage(logger, serverconn)
		if serverconn != nil {
			serverconn.Close()
		}

		time.Sleep(10 * time.Second)

	}
}
func keepAlive(conn net.Conn, childHealthChannel chan bool, logger *logfile.FileLogger) {
	defer func() {
		if conn != nil {
			conn.Close()
		}
	}()

	reader := io.NewNetReadHelper(conn)
	for {
		protocol, err := reader.ReadShort()
		if err != nil || protocol != KEEP_ALIVE_PROTOCOL {
			logger.Println("keepAliveError-1", err)
			return
		}

		msgbytes, err := reader.ReadIntBytesLimit(2048)
		if err != nil {
			logger.Println("keepAliveError-2", err)
			return
		}

		datainputx := whatap_io.NewDataInputX(msgbytes)
		msgmap := value.NewMapValue()
		msgmap.Read(datainputx)

		if msgmap.GetString("Health") == "OK" {
			childHealthChannel <- true
		} else {
			childHealthChannel <- false
		}
	}
}
func monitorChildHealth(childHealthChannel chan bool, logger *logfile.FileLogger) {
	logger.Println("childHealthCheck", "childHealthCheck Start")
	for {
		sockAddr := getAliveSockAddr()
		if err := os.RemoveAll(sockAddr); err != nil {
			logger.Println("childHealthCheckError-1", err)
		}

		l, err := net.Listen("unix", sockAddr)
		if err != nil {
			logger.Println("childHealthCheckError-2", err)
			continue
		}

		errorCount := 0
		for {
			conn, err := l.Accept()
			if err != nil {
				if conn != nil {
					conn.Close()
				}
				logger.Println("childHealthCheckError-3", err)
				errorCount += 1
				time.Sleep(3 * time.Second)
			}

			if errorCount > 10 {
				if conn != nil {
					conn.Close()
				}

				logger.Println("childHealthCheckError-4", "ErrorCount Over 10")
				break
			}

			keepAlive(conn, childHealthChannel, logger)
		}

		l.Close()
		time.Sleep(3 * time.Second)
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
			fmt.Println("mode:foreground")

			// Check if we have a second argument
			if len(os.Args) > 2 {
				arg2 := os.Args[2]
				if arg2 == "debug" {
					fmt.Println("Debug mode: enabled")
					err := os.Setenv("debug", "true")
					if err != nil {
						fmt.Println("error: failed to set env:debug ")
					}
				} else if arg2 == "local-minikube" {
					fmt.Println("Using local minikube configuration")
					// Set the kubeconfig path to the default location
					home := os.Getenv("HOME")
					kubeconfigPath := filepath.Join(home, ".kube", "config")
					k8s.SetKubeconfigPath(kubeconfigPath)

					// Set up HTTP client with Minikube certificates
					if err := client.SetupMinikubeClient(home); err != nil {
						fmt.Printf("Warning: Failed to set up minikube client: %v\n", err)
					}
				}
			}

			//worker
			openHome := os.Getenv("WHATAP_OPEN_HOME")
			logger := logfile.NewFileLogger(logfile.WithOnameLogID("open", "OPEN-AGENT-WORKER"), logfile.WithHomePath(openHome))
			go exitOnStdinClose(logger)
			go keepAliveSender(logger)
			run(openHome, logger)
		}
	}

	// master
	stopper := make(chan os.Signal, 1)
	signal.Notify(stopper, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	// Create a file to indicate that the agent is running
	openHome := os.Getenv("WHATAP_OPEN_HOME")
	logger := logfile.NewFileLogger(logfile.WithOnameLogID("open", "OPEN-AGENT-SUPERVISOR"), logfile.WithHomePath(openHome))
	childHealthChannel := make(chan bool)
	go monitorChildHealth(childHealthChannel, logger)
	time.Sleep(1 * time.Second)

	go superviseRun(childHealthChannel, logger)

	<-stopper
}
