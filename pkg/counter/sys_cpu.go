package counter

import (
	"bufio"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// cpuStat holds parsed /proc/stat values
type cpuStat struct {
	User    uint64
	Nice    uint64
	System  uint64
	Idle    uint64
	Iowait  uint64
	Irq     uint64
	Softirq uint64
	Steal   uint64
}

func (c *cpuStat) total() uint64 {
	return c.User + c.Nice + c.System + c.Idle + c.Iowait + c.Irq + c.Softirq + c.Steal
}

// cpuResult holds calculated CPU percentages
type cpuResult struct {
	Cpu      float32 // total CPU %
	CpuSys   float32
	CpuUsr   float32
	CpuWait  float32
	CpuSteal float32
	CpuIrq   float32
}

var prevCpuStat *cpuStat

// collectCpuPercent reads /proc/stat and calculates CPU usage as delta percentages.
func collectCpuPercent() *cpuResult {
	if runtime.GOOS != "linux" {
		return &cpuResult{}
	}

	cur, err := parseProcStat()
	if err != nil || cur == nil {
		return &cpuResult{}
	}

	result := &cpuResult{}
	if prevCpuStat != nil {
		totalDelta := float64(cur.total() - prevCpuStat.total())
		if totalDelta > 0 {
			idleDelta := float64(cur.Idle - prevCpuStat.Idle)
			result.Cpu = float32((1.0 - idleDelta/totalDelta) * 100)
			result.CpuSys = float32(float64(cur.System-prevCpuStat.System) / totalDelta * 100)
			result.CpuUsr = float32(float64(cur.User-prevCpuStat.User) / totalDelta * 100)
			result.CpuWait = float32(float64(cur.Iowait-prevCpuStat.Iowait) / totalDelta * 100)
			result.CpuSteal = float32(float64(cur.Steal-prevCpuStat.Steal) / totalDelta * 100)
			result.CpuIrq = float32(float64(cur.Irq+cur.Softirq-prevCpuStat.Irq-prevCpuStat.Softirq) / totalDelta * 100)
		}
	}
	prevCpuStat = cur
	return result
}

// parseProcStat reads the first "cpu" line from /proc/stat
func parseProcStat() (*cpuStat, error) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "cpu ") {
			fields := strings.Fields(line)
			if len(fields) < 9 {
				return nil, fmt.Errorf("unexpected /proc/stat format")
			}
			s := &cpuStat{}
			s.User, _ = strconv.ParseUint(fields[1], 10, 64)
			s.Nice, _ = strconv.ParseUint(fields[2], 10, 64)
			s.System, _ = strconv.ParseUint(fields[3], 10, 64)
			s.Idle, _ = strconv.ParseUint(fields[4], 10, 64)
			s.Iowait, _ = strconv.ParseUint(fields[5], 10, 64)
			s.Irq, _ = strconv.ParseUint(fields[6], 10, 64)
			s.Softirq, _ = strconv.ParseUint(fields[7], 10, 64)
			s.Steal, _ = strconv.ParseUint(fields[8], 10, 64)
			return s, nil
		}
	}
	return nil, fmt.Errorf("/proc/stat: cpu line not found")
}

// procCpuState holds state for process CPU calculation
type procCpuState struct {
	prevTime    time.Time
	prevCpuTime float64
}

var procCpu procCpuState

// collectProcCpuPercent calculates the process CPU % from /proc/self/stat
func collectProcCpuPercent(numCpu int32) float32 {
	if runtime.GOOS != "linux" {
		return 0
	}

	now := time.Now()
	cpuTime := readProcSelfCpuTime()

	if procCpu.prevTime.IsZero() {
		procCpu.prevTime = now
		procCpu.prevCpuTime = cpuTime
		return 0
	}

	elapsed := now.Sub(procCpu.prevTime).Seconds()
	if elapsed <= 0 || numCpu <= 0 {
		return 0
	}

	deltaCpu := cpuTime - procCpu.prevCpuTime
	percent := float32((deltaCpu / (elapsed * float64(numCpu))) * 100)

	procCpu.prevTime = now
	procCpu.prevCpuTime = cpuTime
	return percent
}

// readProcSelfCpuTime reads utime + stime from /proc/self/stat (fields 14,15) in seconds
func readProcSelfCpuTime() float64 {
	data, err := os.ReadFile("/proc/self/stat")
	if err != nil {
		return 0
	}

	// /proc/self/stat format: pid (comm) state ... field14(utime) field15(stime) ...
	// comm can contain spaces/parens, so find the closing ')' first
	content := string(data)
	idx := strings.LastIndex(content, ")")
	if idx < 0 || idx+2 >= len(content) {
		return 0
	}

	fields := strings.Fields(content[idx+2:])
	// After ')' and state, fields[0]=state, fields[1]=ppid, ..., fields[11]=utime, fields[12]=stime
	if len(fields) < 13 {
		return 0
	}

	utime, _ := strconv.ParseFloat(fields[11], 64)
	stime, _ := strconv.ParseFloat(fields[12], 64)

	// Convert clock ticks to seconds (typically 100 ticks/sec)
	clkTck := 100.0
	return (utime + stime) / clkTck
}
