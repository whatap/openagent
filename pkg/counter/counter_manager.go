package counter

import (
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/whatap/gointernal/net/secure"
	"github.com/whatap/golib/lang/pack"
	"github.com/whatap/golib/lang/value"
	"github.com/whatap/golib/util/dateutil"

	"open-agent/tools/util/logutil"
)

const (
	counterInterval    = 5000 // 5 seconds in milliseconds
	AGENT_BOOT_ENV     = 2
	OTYPE_INTEGRATIONS = 0x0016
)

// agentStartTime stores the agent process start time in ms
var agentStartTime int64

// agentBootInfo stores the ParamPack for periodic resending
var agentBootInfo *pack.ParamPack

// StartCounterManager starts a goroutine that sends TagCountPack every 5 seconds
func StartCounterManager() {
	agentStartTime = dateutil.Now()
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

	logutil.Infoln("CounterManager", "TagCountPack sender started, interval=5s")

	// === Boot-time sends (1 time) ===
	sendTextPacks()
	sendAgentBootInfo()

	// Align to 5-second boundary
	now := dateutil.Now()
	next := (now/int64(counterInterval))*int64(counterInterval) + int64(counterInterval)

	// Next time to resend agent boot info (every 1 hour)
	nextBootInfoTime := now/dateutil.MILLIS_PER_HOUR*dateutil.MILLIS_PER_HOUR + dateutil.MILLIS_PER_HOUR

	// Next time to resend TextPacks (every 5 minutes)
	nextTextTime := now/dateutil.MILLIS_PER_FIVE_MINUTE*dateutil.MILLIS_PER_FIVE_MINUTE + dateutil.MILLIS_PER_FIVE_MINUTE

	for {
		sleepUntil(next)
		now = dateutil.Now()

		sendTagCountPack(now)
		sendIntegrationsCounter(now)

		// Resend TextPacks every 5 minutes
		if now >= nextTextTime {
			sendTextPacks()
			nextTextTime = now/dateutil.MILLIS_PER_FIVE_MINUTE*dateutil.MILLIS_PER_FIVE_MINUTE + dateutil.MILLIS_PER_FIVE_MINUTE
		}

		// Resend agent boot info every hour
		if now >= nextBootInfoTime {
			resendAgentBootInfo(now)
			nextBootInfoTime = now/dateutil.MILLIS_PER_HOUR*dateutil.MILLIS_PER_HOUR + dateutil.MILLIS_PER_HOUR
		}

		next = (now/int64(counterInterval))*int64(counterInterval) + int64(counterInterval)
	}
}

// sendTextPacks sends TextPacks for ONAME, OKIND, ONODE_NAME registration
func sendTextPacks() {
	defer func() {
		if r := recover(); r != nil {
			logutil.Errorln("CounterManager", "Recovered from panic in sendTextPacks:", r)
		}
	}()

	secu := secure.GetSecurityMaster()
	if secu == nil || secu.PCODE == 0 {
		return
	}

	now := dateutil.Now()

	// TextPack for ONAME
	if len(secu.ONAME) > 0 {
		tp := pack.NewTextPack()
		tp.Pcode = secu.PCODE
		tp.Oid = secu.OID
		tp.Okind = secu.OKIND
		tp.Onode = secu.ONODE
		tp.Time = now
		tp.AddText(pack.TextRec{Div: pack.TEXT_ONAME, Hash: secu.OID, Text: secu.ONAME})
		secure.Send(secure.NET_SECURE_HIDE, tp, true)
		//logutil.Infoln("CounterManager", fmt.Sprintf("Sent TextPack ONAME: hash=%d, text=%s", secu.OID, secu.ONAME))
	}

	// TextPack for OKIND
	if secu.OKIND != 0 && len(secu.OKIND_NAME) > 0 {
		tp := pack.NewTextPack()
		tp.Pcode = secu.PCODE
		tp.Oid = secu.OID
		tp.Okind = secu.OKIND
		tp.Onode = secu.ONODE
		tp.Time = now
		tp.AddText(pack.TextRec{Div: pack.TEXT_OKIND, Hash: secu.OKIND, Text: secu.OKIND_NAME})
		secure.Send(secure.NET_SECURE_HIDE, tp, true)
		//logutil.Infoln("CounterManager", fmt.Sprintf("Sent TextPack OKIND: hash=%d, text=%s", secu.OKIND, secu.OKIND_NAME))
	}

	// TextPack for ONODE_NAME
	if secu.ONODE != 0 && len(secu.ONODE_NAME) > 0 {
		tp := pack.NewTextPack()
		tp.Pcode = secu.PCODE
		tp.Oid = secu.OID
		tp.Okind = secu.OKIND
		tp.Onode = secu.ONODE
		tp.Time = now
		tp.AddText(pack.TextRec{Div: pack.ONODE_NAME, Hash: secu.ONODE, Text: secu.ONODE_NAME})
		secure.Send(secure.NET_SECURE_HIDE, tp, true)
		//logutil.Infoln("CounterManager", fmt.Sprintf("Sent TextPack ONODE_NAME: hash=%d, text=%s", secu.ONODE, secu.ONODE_NAME))
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

	// OS info
	p.PutString("os.name", runtime.GOOS)
	p.PutString("os.arch", runtime.GOARCH)

	logutil.Infoln("CounterManager", fmt.Sprintf("Sending agent boot info: version=%s, os.name=%s, os.arch=%s",
		os.Getenv("WHATAP_VERSION"), runtime.GOOS, runtime.GOARCH))

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

// sendTagCountPack sends TagCountPack with category "common_agent_info"
func sendTagCountPack(now int64) {
	defer func() {
		if r := recover(); r != nil {
			logutil.Errorln("CounterManager", "Recovered from panic in sendTagCountPack:", r)
		}
	}()

	secu := secure.GetSecurityMaster()
	if secu == nil || secu.PCODE == 0 {
		return
	}

	p := pack.NewTagCountPack()

	// AbstractPack common fields
	p.Pcode = secu.PCODE
	p.Oid = secu.OID
	p.Okind = secu.OKIND
	p.Onode = secu.ONODE
	p.Time = now

	// TagCountPack specific
	p.Category = "common_agent_info"

	// Tags: agent identification (must use numeric types for yard-side getInt() parsing)
	p.Tags.Put("otype", value.NewDecimalValue(int64(OTYPE_INTEGRATIONS)))
	p.Tags.Put("subType", value.NewDecimalValue(1))
	p.Tags.Put("hostIp", value.NewDecimalValue(int64(secu.IP)))
	p.Tags.Put("startTime", value.NewDecimalValue(agentStartTime))
	cpuCores := getCgroupCpuLimitFloat()
	if cpuCores <= 0 {
		cpuCores = float64(runtime.NumCPU())
	}
	p.Tags.Put("cpuCores", value.NewDecimalValue(int64(cpuCores)))

	//// Tags: name information (for server-side resolution)
	//p.PutTag("oname", secu.ONAME)
	//p.PutTag("okindName", secu.OKIND_NAME)
	//p.PutTag("onodeName", secu.ONODE_NAME)
	//
	//// Fields: runtime metrics
	//var memStats runtime.MemStats
	//runtime.ReadMemStats(&memStats)
	//p.Put("heapUsed", int64(memStats.HeapInuse))
	//p.Put("heapTotal", int64(memStats.HeapSys))
	//p.Put("goroutineCount", int32(runtime.NumGoroutine()))
	//p.Put("pid", int32(os.Getpid()))

	// Send with immediate flush
	secure.Send(secure.NET_SECURE_HIDE, p, true)
}

// sendIntegrationsCounter sends TagCountPack with category "integrations_counter"
func sendIntegrationsCounter(now int64) {
	defer func() {
		if r := recover(); r != nil {
			logutil.Errorln("CounterManager", "Recovered from panic in sendIntegrationsCounter:", r)
		}
	}()

	secu := secure.GetSecurityMaster()
	if secu == nil || secu.PCODE == 0 {
		return
	}

	p := pack.NewTagCountPack()
	p.Pcode = secu.PCODE
	p.Oid = secu.OID
	p.Okind = secu.OKIND
	p.Onode = secu.ONODE
	p.Time = now
	p.Category = "integrations_counter"

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
