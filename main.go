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
	"net/http"
	_ "net/http/pprof"
	"open-agent/open"
	"open-agent/pkg/config"
	"open-agent/util/io"
	"os"
	"os/exec"
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
func startWorker(command string, logger *logfile.FileLogger) (*exec.Cmd, error) {
	var cmd *exec.Cmd
	if config.IsForceStandaloneMode() {
		logger.Infoln("StartWorker", fmt.Sprintf("Start Worker Process(%s foreground standalone)", command))
		cmd = exec.Command(command, "foreground", "standalone")
	} else {
		logger.Infoln("StartWorker", fmt.Sprintf("Start Worker Process(%s foreground)", command))
		cmd = exec.Command(command, "foreground")
	}
	err := cmd.Start()
	if err != nil {
		logger.Infoln("StartWorkerError", err)
		return nil, err
	}
	return cmd, nil
}
func superviseRun(childHealthChannel chan bool, logger *logfile.FileLogger) {
	for {
		logger.Infoln("superviseRun", "Worker Start")
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
					logger.Infoln("superviseCheck", "child health problem, child kill")
				}
			case <-time.After(time.Duration(3) * time.Minute):
				if cmd != nil && cmd.Process != nil {
					cmd.Process.Signal(syscall.SIGABRT)
					err = cmd.Wait()
					childAlive = false
				}
				logger.Infoln("superviseCheck", "child health check timeout, child kill")
			}
		}

		if err != nil {
			logger.Infoln("superviseRunError", "command wait error : ", err)
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
			logger.Infoln("KeepAlive", "HealthCheck Fail")
			msgmap.PutString("Health", "PROBLEM")
		}

		dout := whatap_io.NewDataOutputX()
		dout.WriteShort(KEEP_ALIVE_PROTOCOL)
		doutx := whatap_io.NewDataOutputX()
		msgmap.Write(doutx)
		dout.WriteIntBytes(doutx.ToByteArray())
		err = writer.WriteBytes(dout.ToByteArray(), 30*time.Second)
		if err != nil {
			logger.Infoln("KeepAliveWriteFail", "KeepAliveFail")
		}

		time.Sleep(60 * time.Second)
	}
}
func keepAliveSender(logger *logfile.FileLogger) {
	// 루프 밖에서 소켓 주소를 한 번만 계산합니다.
	sockAddr := getAliveSockAddr()
	logger.Infoln("keepAliveSender", "Socket address initially set to", sockAddr)

	for {
		logger.Infoln("keepAliveSender", "keepAliveSender loop starts")

		// 소켓 파일 존재 여부 확인
		if _, err := os.Stat(sockAddr); os.IsNotExist(err) {
			logger.Infoln("keepAliveSenderError-1", "keepAliveSocketNotExist, re-evaluating socket address", sockAddr)
			// 파일이 없으면 주소를 다시 계산하고 잠시 대기 후 루프를 다시 시작합니다.
			sockAddr = getAliveSockAddr()
			time.Sleep(10 * time.Second)
			continue
		}

		// 연결 시도
		socktype := "unix"
		laddr := net.UnixAddr{sockAddr, socktype}
		conn, err := net.DialUnix(socktype, nil, &laddr)

		// 연결 실패 처리
		if err != nil {
			logger.Infoln("keepAliveSenderError-2", "Dial Error", err)
			time.Sleep(3 * time.Second)
			continue // 루프의 처음으로 돌아가 다시 시도
		}

		// 연결 성공 시 메시지 전송
		sendKeepAliveMessage(logger, conn)
		conn.Close()

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
			logger.Infoln("keepAliveError-1", err)
			return
		}

		msgbytes, err := reader.ReadIntBytesLimit(2048)
		if err != nil {
			logger.Infoln("keepAliveError-2", err)
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
	logger.Infoln("childHealthCheck", "childHealthCheck Start")
	for {
		sockAddr := getAliveSockAddr()
		if err := os.RemoveAll(sockAddr); err != nil {
			logger.Println("childHealthCheckError-1", err)
		}

		l, err := net.Listen("unix", sockAddr)
		if err != nil {
			logger.Infoln("childHealthCheckError-2", err)
			continue
		}

		errorCount := 0
		for {
			conn, err := l.Accept()
			if err != nil {
				if conn != nil {
					conn.Close()
				}
				logger.Infoln("childHealthCheckError-3", err)
				errorCount += 1
				time.Sleep(3 * time.Second)
			}

			if errorCount > 10 {
				if conn != nil {
					conn.Close()
				}

				logger.Infoln("childHealthCheckError-4", "ErrorCount Over 10")
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
			logger := logfile.NewFileLogger(logfile.WithOnameLogID("open", "OPEN-AGENT-WORKER"), logfile.WithHomePath(openHome))
			go exitOnStdinClose(logger)
			go keepAliveSender(logger)
			run(openHome, logger)
		} else if arg1 == "standalone" {
			if config.IsDebugEnabled() {
				fmt.Println("Standalone supervisor mode: enabled")
			}
			// Set force standalone mode for supervisor
			config.SetForceStandaloneMode(true)
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
