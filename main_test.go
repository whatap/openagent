//go:build unix

package main

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/whatap/golib/logger/logfile"
)

// The tests below run main() in a child copy of the test binary, because
// signal handling and the foreground/default mode dispatch are process-wide.
const (
	helperArgsEnv  = "OPENAGENT_MAIN_TEST_ARGS"
	helperBootEnv  = "OPENAGENT_MAIN_TEST_BOOT"
	stubBootMarker = "OPENAGENT_MAIN_TEST_BOOT_STARTED"

	// bootReturns models a boot that finished; bootBlocks models
	// secure.StartNet waiting for a collector connection that never opens.
	bootReturns = "returns"
	bootBlocks  = "blocks"

	agentReadyTimeout = 20 * time.Second
	shutdownTimeout   = 5 * time.Second
	outputPollPeriod  = 50 * time.Millisecond
)

func TestMain(m *testing.M) {
	if args := os.Getenv(helperArgsEnv); args != "" {
		runMainAsAgent(args)
		return
	}
	os.Exit(m.Run())
}

func runMainAsAgent(args string) {
	blockBoot := os.Getenv(helperBootEnv) == bootBlocks
	bootAgent = func(string, string, string, *logfile.FileLogger) {
		fmt.Println(stubBootMarker)
		if blockBoot {
			select {}
		}
	}
	os.Args = append([]string{"openagent"}, strings.Fields(args)...)
	main()
	os.Exit(0)
}

// SIGTERM must stop the agent even while BootOpenAgent is still waiting for
// the first collector connection (secure.StartNet blocks until it opens).
func TestSIGTERMStopsAgentWhileBootIsBlocked(t *testing.T) {
	for _, mode := range []string{"standalone", "foreground", "foreground standalone"} {
		t.Run(mode, func(t *testing.T) {
			agent := startAgent(t, mode, bootBlocks)
			agent.waitForOutput(t, stubBootMarker)

			agent.terminate(t)

			agent.requireCleanExit(t)
		})
	}
}

// After a completed boot, one SIGTERM must end the process in every mode.
// The foreground worker used to fall through into the default mode and boot
// a second agent instead of exiting.
func TestSIGTERMStopsBootedAgentWithoutRestart(t *testing.T) {
	for _, mode := range []string{"standalone", "foreground", "foreground standalone"} {
		t.Run(mode, func(t *testing.T) {
			agent := startAgent(t, mode, bootReturns)
			agent.waitForOutput(t, stubBootMarker)

			agent.terminate(t)

			agent.requireCleanExit(t)
			if boots := strings.Count(agent.output(), stubBootMarker); boots != 1 {
				t.Fatalf("agent booted %d times after one SIGTERM, want 1\n%s", boots, agent.output())
			}
		})
	}
}

type agentProcess struct {
	cmd     *exec.Cmd
	out     *lockedBuffer
	done    chan struct{} // closed after cmd.Wait returns
	waitErr error
}

func startAgent(t *testing.T, mode, boot string) *agentProcess {
	t.Helper()
	home := newAgentHome(t)
	cmd := exec.Command(os.Args[0])
	cmd.Dir = home
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + home,
		"WHATAP_HOME=" + home,
		"WHATAP_OPEN_HOME=" + home,
		"PPROF_PORT=0",
		helperArgsEnv + "=" + mode,
		helperBootEnv + "=" + boot,
	}
	out := &lockedBuffer{}
	cmd.Stdout = out
	cmd.Stderr = out
	if err := cmd.Start(); err != nil {
		t.Fatalf("start agent: %v", err)
	}
	agent := &agentProcess{cmd: cmd, out: out, done: make(chan struct{})}
	go func() {
		agent.waitErr = cmd.Wait()
		close(agent.done)
	}()
	t.Cleanup(func() {
		select {
		case <-agent.done:
		default:
			_ = cmd.Process.Kill()
			<-agent.done
		}
	})
	return agent
}

// newAgentHome writes a minimal whatap.conf that points at a closed local
// port, so nothing can reach a real collector even if the boot stub changes.
func newAgentHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "logs"), 0o755); err != nil {
		t.Fatal(err)
	}
	conf := fmt.Sprintf("license=main-shutdown-test\nwhatap.server.host=127.0.0.1\nwhatap.server.port=%d\n", closedLocalPort(t))
	writeFile(t, filepath.Join(home, "whatap.conf"), conf)
	writeFile(t, filepath.Join(home, "scrape_config.yaml"), "features:\n  openAgent:\n    enabled: true\n    targets: []\n")
	return home
}

func closedLocalPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}
	return portNumber
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (a *agentProcess) waitForOutput(t *testing.T, marker string) {
	t.Helper()
	deadline := time.Now().Add(agentReadyTimeout)
	for time.Now().Before(deadline) {
		if strings.Contains(a.output(), marker) {
			return
		}
		select {
		case <-a.done:
			t.Fatalf("agent exited before %q: %v\n%s", marker, a.waitErr, a.output())
		case <-time.After(outputPollPeriod):
		}
	}
	t.Fatalf("agent did not print %q within %s\n%s", marker, agentReadyTimeout, a.output())
}

func (a *agentProcess) terminate(t *testing.T) {
	t.Helper()
	if err := a.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("send SIGTERM: %v", err)
	}
}

func (a *agentProcess) requireCleanExit(t *testing.T) {
	t.Helper()
	select {
	case <-a.done:
		if a.waitErr != nil {
			t.Fatalf("agent exited with %v after SIGTERM, want exit status 0\n%s", a.waitErr, a.output())
		}
	case <-time.After(shutdownTimeout):
		_ = a.cmd.Process.Kill()
		<-a.done
		t.Fatalf("agent still running %s after SIGTERM\n%s", shutdownTimeout, a.output())
	}
}

func (a *agentProcess) output() string {
	return a.out.String()
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
