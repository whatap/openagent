package control

import (
	"bufio"
	"math"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	"open-agent/pkg/config"
	"open-agent/tools/util/logutil"

	"github.com/whatap/gointernal/net/secure"
	"github.com/whatap/golib/lang/pack"
	"github.com/whatap/golib/lang/value"
	"github.com/whatap/golib/logger/logfile"
	"github.com/whatap/golib/util/fileutil"
)

var started bool = false
var appLogger *logfile.FileLogger

// InitControlHandler starts the control handler goroutine
func InitControlHandler(logger *logfile.FileLogger) {
	if started {
		return
	}
	started = true
	appLogger = logger

	secure.InitReceiver()
	go runControl()
}

func runControl() {
	for {
		func() {
			defer func() {
				if r := recover(); r != nil {
					logutil.Println("WA811-01", "ControlHandler Recover ", r)
					logutil.Println("WA811-01", string(debug.Stack()))
				}
			}()

			p := <-secure.RecvBuffer
			switch p.GetPackType() {
			case pack.PACK_PARAMETER:
				process(p.(*pack.ParamPack))
			}
		}()
	}
}

func process(p *pack.ParamPack) {
	defer func() {
		if r := recover(); r != nil {
			logutil.Println("WA811-02", "process Recover ", r)
			logutil.Println("WA811-02", string(debug.Stack()))
		}
	}()

	debugEnabled := config.IsDebugEnabled()

	switch p.Id {
	case secure.GET_ENV:
		if debugEnabled {
			logutil.Infoln("CONTROL", "GET_ENV")
		}
		processGetEnv(p)

	case secure.CONFIGURE_GET:
		if debugEnabled {
			logutil.Infoln("CONTROL", "CONFIGURE_GET")
		}
		processConfigureGet(p)

	case secure.SET_CONFIG:
		if debugEnabled {
			logutil.Infoln("CONTROL", "SET_CONFIG")
		}
		processSetConfig(p)

	case secure.AGENT_LOG_LIST:
		if debugEnabled {
			logutil.Infoln("CONTROL", "AGENT_LOG_LIST")
		}
		processAgentLogList(p)

	case secure.AGENT_LOG_READ:
		if debugEnabled {
			logutil.Infoln("CONTROL", "AGENT_LOG_READ")
		}
		processAgentLogRead(p)

	default:
		if debugEnabled {
			logutil.Infof("CONTROL", "Unknown command ID: %d", p.Id)
		}
		return
	}

	secure.Send(secure.NET_SECURE_HIDE, p.ToResponse(), true)
}

// processGetEnv handles GET_ENV command - returns environment variables
func processGetEnv(p *pack.ParamPack) {
	m := value.NewMapValue()

	for _, e := range os.Environ() {
		idx := strings.Index(e, "=")
		if idx > 0 {
			m.PutString(e[:idx], e[idx+1:])
		}
	}

	// Add version info
	ver := os.Getenv("WHATAP_VERSION")
	if ver != "" {
		m.PutString("whatap.agent_version", ver)
	}

	p.Put("env", m)
}

// processConfigureGet handles CONFIGURE_GET command - returns whatap.conf contents
func processConfigureGet(p *pack.ParamPack) {
	configType := p.GetString("config_type")

	switch configType {
	case "text":
		path := getConfFile()
		if data, _, err := fileutil.ReadFile(path, 64*1024); err == nil {
			p.Put("contents", value.NewTextValue(string(data)))
		} else {
			logutil.Println("WA811-03", "CONFIGURE_GET read file error: ", err)
			p.Put("contents", value.NewNullValue())
		}
	default:
		// Return as key-value map (property format)
		configMap := config.GetConfigMap()
		m := value.NewMapValue()
		for key, val := range configMap {
			if strings.HasPrefix(key, "_") || strings.Contains(key, "OID") {
				continue
			}
			m.PutString(key, val)
		}
		p.SetMapValue(m)
	}
}

// processSetConfig handles SET_CONFIG command - updates whatap.conf
func processSetConfig(p *pack.ParamPack) {
	configType := p.GetString("config_type")

	switch configType {
	case "text":
		path := getConfFile()
		contents := p.GetString("contents")
		if err := fileutil.ReplaceFileWithOldBackup(path, contents); err != nil {
			logutil.Println("WA811-04", "SET_CONFIG text error: ", err)
		} else {
			config.GetInstance().LoadConfig()
		}
	default:
		configMap := p.GetMap("config")
		if configMap != nil {
			newValues := map[string]string{}
			keyEnumer := configMap.Keys()
			for keyEnumer.HasMoreElements() {
				key := keyEnumer.NextString()
				newValues[key] = configMap.GetString(key)
			}
			mergeWriteConfig(newValues)
			config.GetInstance().LoadConfig()
		}
	}
}

// mergeWriteConfig reads the existing whatap.conf line by line,
// updates matching keys while preserving comments and ordering,
// and appends new keys at the end.
func mergeWriteConfig(newValues map[string]string) {
	path := getConfFile()

	f, err := os.OpenFile(path, os.O_RDWR, 0644)
	if err != nil {
		logutil.Println("WA811-04", "SET_CONFIG open error: ", err)
		return
	}
	defer f.Close()

	// Read existing file line by line
	scanner := bufio.NewScanner(f)
	existingKeys := map[string]bool{}
	var result string

	for scanner.Scan() {
		line := scanner.Text()

		// Lines without '=' (comments, blank lines) - keep as-is
		if !strings.Contains(line, "=") {
			result += line + "\n"
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		existingKeys[key] = true

		// If key starts with a word char (not a comment like #key=val)
		if len(key) > 0 && key[0] != '#' {
			if newVal, ok := newValues[key]; ok {
				if strings.TrimSpace(newVal) != "" {
					val = newVal
				}
			}
		}

		if strings.TrimSpace(val) != "" {
			result += key + "=" + val + "\n"
		}
	}

	// Append new keys that didn't exist in the file
	for key, val := range newValues {
		if existingKeys[key] {
			continue
		}
		if strings.TrimSpace(key) != "" && strings.TrimSpace(val) != "" {
			result += key + "=" + val + "\n"
		}
	}

	// Truncate and write back
	wf, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		logutil.Println("WA811-04", "SET_CONFIG write error: ", err)
		return
	}
	defer wf.Close()
	wf.WriteString(result)
}

// processAgentLogList handles AGENT_LOG_LIST command - returns log file list
func processAgentLogList(p *pack.ParamPack) {
	m := value.NewMapValue()

	// Use the appLogger's GetLogFiles which matches OPEN-AGENT-open-*.log pattern
	if appLogger != nil {
		logFiles := appLogger.GetLogFiles()
		if logFiles != nil {
			keys := logFiles.Keys()
			for keys.HasMoreElements() {
				key := keys.NextString()
				m.Put(key, logFiles.Get(key))
			}
		}
	}

	// Also include whatap-boot-*.log files (boot logger uses different prefix)
	addLogFilesByPrefix(m, "whatap-boot")

	p.Put("files", m)
}

// addLogFilesByPrefix adds log files matching a given prefix to the map
func addLogFilesByPrefix(m *value.MapValue, prefix string) {
	home := os.Getenv("WHATAP_HOME")
	if home == "" {
		home = os.Getenv("WHATAP_OPEN_HOME")
		if home == "" {
			home = "."
		}
	}
	searchDir := filepath.Join(home, "logs")

	files, err := os.ReadDir(searchDir)
	if err != nil {
		return
	}

	for _, f := range files {
		if f.IsDir() {
			continue
		}
		name := f.Name()
		if !strings.HasPrefix(name, prefix+"-") {
			continue
		}
		// Validate date format in filename
		x := strings.LastIndex(name, ".")
		if x < 0 {
			continue
		}
		s := strings.LastIndex(name, "-")
		if s < 0 || s >= x-1 {
			continue
		}
		date := name[s+1 : x]
		if len(date) != 8 {
			continue
		}

		info, err := f.Info()
		if err != nil {
			continue
		}
		m.Put(name, value.NewDecimalValue(info.Size()))

		if m.Size() >= 100 {
			break
		}
	}
}

// processAgentLogRead handles AGENT_LOG_READ command - reads log file contents
func processAgentLogRead(p *pack.ParamPack) {
	file := p.GetString("file")
	endpos := p.GetLong("pos")
	length := p.GetLong("length")
	length = int64(math.Min(float64(length), 8000))

	if config.IsDebugEnabled() {
		logutil.Infof("CONTROL", "AGENT_LOG_READ file=%s, pos=%d, length=%d", file, endpos, length)
	}

	// Security: prevent directory traversal
	if strings.Contains(file, "..") || strings.Contains(file, "/") || strings.Contains(file, "\\") {
		logutil.Println("WA811-05", "AGENT_LOG_READ invalid file path: ", file)
		return
	}

	// Try reading via appLogger first (handles OPEN-AGENT-open-*.log)
	if appLogger != nil {
		logData := appLogger.Read(file, endpos, length)
		if logData != nil {
			p.PutLong("before", logData.Before)
			p.PutLong("next", logData.Next)
			p.PutString("text", logData.Text)
			return
		}
	}

	// Fallback: read directly from logs directory (for whatap-boot-*.log etc.)
	home := os.Getenv("WHATAP_HOME")
	if home == "" {
		home = os.Getenv("WHATAP_OPEN_HOME")
		if home == "" {
			home = "."
		}
	}
	searchFilePath := filepath.Join(home, "logs", file)

	f, err := os.Open(searchFilePath)
	if err != nil {
		logutil.Println("WA811-06", "AGENT_LOG_READ open error: ", err)
		return
	}
	defer f.Close()

	fInfo, err := f.Stat()
	if err != nil {
		return
	}
	if fInfo.Size() < endpos {
		return
	}

	if endpos < 0 {
		endpos = fInfo.Size()
	}
	start := int64(math.Max(0, float64(endpos-length)))

	available := fInfo.Size() - start
	readable := int(math.Min(float64(available), float64(length)))

	buff := make([]byte, readable)
	n, err := f.ReadAt(buff, start)
	if err != nil {
		logutil.Println("WA811-07", "AGENT_LOG_READ read error: ", err)
		return
	}

	next := start + int64(n)
	if (next + length) > fInfo.Size() {
		next = -1
	} else {
		next += length
	}

	p.PutLong("before", start)
	p.PutLong("next", next)
	p.PutString("text", string(buff))
}

// getConfFile returns the path to whatap.conf
func getConfFile() string {
	home := os.Getenv("WHATAP_HOME")
	if home == "" {
		home = os.Getenv("WHATAP_OPEN_HOME")
		if home == "" {
			home = "."
		}
	}
	return filepath.Join(home, "whatap.conf")
}

// getLogHome returns the log home directory
func getLogHome() string {
	home := os.Getenv("WHATAP_HOME")
	if home == "" {
		home = os.Getenv("WHATAP_OPEN_HOME")
		if home == "" {
			home = "."
		}
	}
	return home
}
