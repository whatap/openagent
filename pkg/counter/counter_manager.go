package counter

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"time"

	"github.com/whatap/gointernal/net/secure"
	"github.com/whatap/golib/lang/pack"
	"github.com/whatap/golib/util/dateutil"

	"open-agent/tools/util/logutil"
)

const (
	counterInterval = 5000 // 5 seconds in milliseconds
	AGENT_BOOT_ENV  = 2
)

// agentBootInfo stores the ParamPack for periodic resending
var agentBootInfo *pack.ParamPack

// StartCounterManager starts a goroutine that sends CounterPack1 every 5 seconds
func StartCounterManager() {
	go runCounter()
}

func runCounter() {
	defer func() {
		if r := recover(); r != nil {
			logutil.Errorln("CounterManager", "Recovered from panic:", r)
		}
	}()

	// Wait until SecurityMaster is initialized (Cypher ready)
	secu := secure.GetSecurityMaster()
	secu.WaitForInit()

	// Wait until DecideAgentOnameOid completes (OID assigned, env vars set)
	for secu.OID == 0 {
		time.Sleep(500 * time.Millisecond)
	}

	logutil.Infoln("CounterManager", "CounterPack1 sender started, interval=5s")

	// Send agent boot info (ParamPack with version, oname, etc.)
	sendAgentBootInfo()

	// Initialize first CPU reading (delta needs two data points)
	collectCpuPercent()

	// Align to 5-second boundary
	now := dateutil.Now()
	next := (now/int64(counterInterval))*int64(counterInterval) + int64(counterInterval)

	// Next time to resend agent boot info (every 1 hour)
	nextBootInfoTime := now/dateutil.MILLIS_PER_HOUR*dateutil.MILLIS_PER_HOUR + dateutil.MILLIS_PER_HOUR

	for {
		sleepUntil(next)
		now = dateutil.Now()

		sendCounterPack(now)

		// Resend agent boot info every hour
		if now >= nextBootInfoTime {
			resendAgentBootInfo(now)
			nextBootInfoTime = now/dateutil.MILLIS_PER_HOUR*dateutil.MILLIS_PER_HOUR + dateutil.MILLIS_PER_HOUR
		}

		next = (now/int64(counterInterval))*int64(counterInterval) + int64(counterInterval)
	}
}

// sendAgentBootInfo sends agent information via ParamPack at startup
func sendAgentBootInfo() {
	defer func() {
		if r := recover(); r != nil {
			logutil.Errorln("CounterManager", "Recovered from panic in sendAgentBootInfo:", r)
		}
	}()

	secu := secure.GetSecurityMaster()
	if secu == nil || secu.PCODE == 0 {
		return
	}

	now := dateutil.Now()

	p := pack.NewParamPack()
	p.Pcode = secu.PCODE
	p.Oid = secu.OID
	p.Okind = secu.OKIND
	p.Onode = secu.ONODE
	p.Time = now
	p.Id = AGENT_BOOT_ENV

	// Agent version
	p.PutString("whatap.version", os.Getenv("WHATAP_VERSION"))

	// Start time
	os.Setenv("whatap.starttime", strconv.FormatInt(now, 10))
	p.PutString("whatap.starttime", os.Getenv("whatap.starttime"))

	// Agent identity
	p.PutString("whatap.oname", secu.ONAME)
	p.PutString("whatap.name", os.Getenv("whatap.name"))
	p.PutString("whatap.ip", int32ToIP(secu.IP))
	hostName, _ := os.Hostname()
	p.PutString("whatap.hostname", hostName)
	p.PutString("whatap.type", os.Getenv("whatap.type"))
	p.PutString("whatap.pid", strconv.Itoa(os.Getpid()))

	// OS info
	p.PutString("os.arch", runtime.GOARCH)
	p.PutString("os.name", runtime.GOOS)
	cpuCores := getCgroupCpuLimitFloat()
	if cpuCores <= 0 {
		cpuCores = float64(runtime.NumCPU())
	}
	p.PutString("os.cpucore", strconv.FormatFloat(cpuCores, 'f', -1, 64))

	logutil.Infoln("CounterManager", fmt.Sprintf("Sending agent boot info: version=%s, oname=%s",
		os.Getenv("WHATAP_VERSION"), secu.ONAME))

	// Send with flush
	secure.Send(secure.NET_SECURE_HIDE, p, true)

	// Store for periodic resending
	agentBootInfo = p
}

// resendAgentBootInfo resends stored agent boot info (every 1 hour)
func resendAgentBootInfo(now int64) {
	if agentBootInfo == nil {
		return
	}
	agentBootInfo.Id = AGENT_BOOT_ENV
	agentBootInfo.Time = now
	secure.Send(secure.NET_SECURE_HIDE, agentBootInfo, true)
}

func sendCounterPack(now int64) {
	defer func() {
		if r := recover(); r != nil {
			logutil.Errorln("CounterManager", "Recovered from panic in sendCounterPack:", r)
		}
	}()

	secu := secure.GetSecurityMaster()
	if secu == nil || secu.PCODE == 0 {
		return
	}

	p := pack.NewCounterPack1()

	// Identification
	p.Pcode = secu.PCODE
	p.Oid = secu.OID
	p.Okind = secu.OKIND
	p.Onode = secu.ONODE
	p.Time = now
	p.Duration = 5

	// CPU cores (container-aware)
	p.CpuCores = GetCPUCores()

	// System CPU %
	//cpuInfo := collectCpuPercent()
	//p.Cpu = cpuInfo.Cpu
	//p.CpuSys = cpuInfo.CpuSys
	//p.CpuUsr = cpuInfo.CpuUsr
	//p.CpuWait = cpuInfo.CpuWait
	//p.CpuSteal = cpuInfo.CpuSteal
	//p.CpuIrq = cpuInfo.CpuIrq

	// System memory %
	//memInfo := collectMemPercent()
	//p.Mem = memInfo.Mem
	//p.Swap = memInfo.Swap

	// Process CPU %
	//p.CpuProc = collectProcCpuPercent(p.CpuCores)

	// Host IP (int32 from SecurityMaster, set during TCP connection)
	p.HostIp = secu.IP

	// Pack version
	p.Version = 1

	// Go runtime heap memory
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	p.HeapUse = int64(memStats.HeapInuse)
	p.HeapTot = int64(memStats.HeapSys)

	// Thread (goroutine) count
	p.ThreadCount = int32(runtime.NumGoroutine())

	// PID
	p.Pid = int32(os.Getpid())

	// Starttime
	p.Starttime, _ = strconv.ParseInt(os.Getenv("whatap.starttime"), 10, 64)

	// Send with immediate flush
	secure.Send(secure.NET_SECURE_HIDE, p, true)
}

func sleepUntil(targetMs int64) {
	for {
		now := dateutil.Now()
		diff := targetMs - now
		if diff <= 0 {
			return
		}
		if diff > 3000 {
			diff = 3000
		}
		time.Sleep(time.Duration(diff) * time.Millisecond)
	}
}

// int32ToIP converts an int32 IP (same as secu.IP) to dotted string
func int32ToIP(ip int32) string {
	return fmt.Sprintf("%d.%d.%d.%d",
		byte(ip>>24), byte(ip>>16), byte(ip>>8), byte(ip))
}
