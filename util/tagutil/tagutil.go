package tagutil

import (
	"bufio"
	"debug/elf"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"npmagent/k8s"
	"npmagent/util/hashutil"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/whatap/gointernal/net/secure"

	ps "github.com/shirou/gopsutil/v3/process"
	"github.com/whatap/golib/lang/pack"
	"github.com/whatap/golib/lang/value"
	"github.com/whatap/golib/logger/logfile"
	"github.com/whatap/golib/util/dateutil"
	"gopkg.in/yaml.v2"
)

const (
	PARAM_TAGRULE = 600
)

func GetMyIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		log.Println(err)
	}
	defer conn.Close()
	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP.String()
}

func GetHostName() (string, error) {
	return os.Hostname()
}

type ProcessInfo struct {
	Pid            int32
	ProcessName    string
	ProcessTagName string
	ProcessType    string
	AppName        string
	LanguageType   string
	AddInfoType    string
	HostName       string
	//K8S
	Namespace     string
	PodName       string
	ContainerName string
	PodID         string
	ContainerID   string
	//AppType        string
}

type AppNameKey struct {
	ProcessTag  string `yaml:"process_tag"`
	ProcessType string `yaml:"process_type"`
	HostName    string `yaml:"host_tag"`
	IP          string `yaml:"ip"`
	Port        string `yaml:"port"`
}

var PMapLock = &sync.Mutex{}

// TODO PROCESS가 너무 많아지면 안쓰는 정보 clear 검토 필요
var ProcessInfoMap = make(map[int32]*ProcessInfo)
var ProcessMap = make(map[string]string)

var AppNameMap map[string][]AppNameKey
var AppNameDefault string

var pTagRegex *regexp.Regexp

var goTagRegex *regexp.Regexp
var javaTagRegex *regexp.Regexp

// processWhiteList  = make(map[string]int)
var whiteList *regexp.Regexp

// processBlackList  = make(map[string]int)
var blackList *regexp.Regexp
var processAll = 1

func NewProcessInfoMap() {
	ProcessInfoMap = make(map[int32]*ProcessInfo)
}
func ProcessPostInfo(pid int32, pName string, p *ps.Process, pInfo *ProcessInfo) error {

	pInfo.ProcessType = "Unknown"

	// Java
	if pName == "java" {
		cmds, err := p.CmdlineSlice()
		if err != nil {
			return err
		}

		javaName := "java"
		javaType := ""

		cp := false
		for i := 0; i < len(cmds); i++ {
			if strings.Index(cmds[i], "-") == 0 {
				if strings.Index(cmds[i], "cp") == 1 || strings.Index(cmds[i], "classpath") == 1 {
					//path := cmds[i+1]
					cp = true
					i += 1

				} else if strings.Index(cmds[i], "D") == 1 {
					if javaTagRegex == nil {
						continue
					}
					javaTag := javaTagRegex.FindString(cmds[i])
					if javaTag != "" {
						javaType = ProcessMap[javaTag]
					}
				}
			} else {
				if strings.Index(cmds[i], ".jar") != -1 {
					javaName = cmds[i]
				} else if cp == true {
					javaName = cmds[i]
					cp = false
				}
			}
		}

		pInfo.ProcessName = javaName
		javaTag := ""
		if javaTagRegex != nil {
			javaTag = javaTagRegex.FindString(javaName)
		}
		if javaTag == "" {
			if javaType == "" {
				pInfo.ProcessType = javaName
			} else {
				pInfo.ProcessType = javaType
			}
		} else {
			pInfo.ProcessType = ProcessMap[javaTag]
		}

		pInfo.AddInfoType = javaTag + javaName
		pInfo.LanguageType = "java"

		return nil
	}

	exe, err := p.Exe()
	// Python
	if strings.Contains(pName, "python") {
		cmds, err := p.CmdlineSlice()
		if err != nil {
			return err
		}

		if len(cmds) < 2 {
			return errors.New(fmt.Sprintf("Python Name error"))
		}

		path := strings.Split(cmds[1], "/")

		pInfo.ProcessName = path[len(path)-1]
		pInfo.LanguageType = "python"
		return nil
	}

	if strings.Contains(exe, "python") {
		pInfo.LanguageType = "python"
		return nil
	}

	// Bash
	if strings.Contains(exe, "bash") && pName != "bash" {
		pInfo.LanguageType = "bash script"
		return nil
	}

	// SymbolCheck

	exeSlice := strings.Split(exe, "/")
	if exeSlice[len(exeSlice)-1] == pName {
		exe = fmt.Sprintf("/proc/%d/root/%s", pid, exe)

		if err != nil {
			return err
		}

		e, err := elf.Open(exe)

		if err == nil {
			// GO
			s := e.Section(".note.go.buildid")
			if s != nil {
				pInfo.LanguageType = "go"
				ss, _ := e.Symbols()
				for _, s := range ss {
					if elf.SymType(s.Info) == elf.STT_OBJECT {
						goTagName := ""
						if goTagRegex != nil {
							goTagName = goTagRegex.FindString(s.Name)
						}
						if goTagName != "" {
							pInfo.AddInfoType = goTagName
							pInfo.ProcessType = ProcessMap[goTagName]
							break
						}
					}
				}

				return nil
			}

			// C
			s = e.Section(".note.gnu.build-id")
			if s != nil {
				pInfo.LanguageType = "c/c++"
				return nil
			}
		}
	}
	// PHP - TODO

	// UNKNOWN

	return nil
}

func SetProcessList(whiteStrList, blackStrList []string) {

	if len(whiteStrList) == 0 {
		processAll = 1
	} else {
		processAll = 0
		regex := strings.Join(whiteStrList, "|")
		whiteList, _ = regexp.Compile(regex)
	}

	if len(blackStrList) > 0 {
		regex := strings.Join(blackStrList, "|")
		blackList, _ = regexp.Compile(regex)
	} else {
		blackList = nil
	}

}

func SetProcessTagRegex(regexRule string) {
	pTagRegex, _ = regexp.Compile(regexRule)

}

func SetMappingTableAppName(mappingRule map[string][]AppNameKey) {
	AppNameMap = mappingRule
}

func SetMappingTableProcessType(mappingRule map[string][]string) {

	javaArr := []string{"spring", "kafka", "zookeeper"}
	goArr := []string{"gin", "sarama", "chi", "gorm", "fiber", "redigo", "gorilla", "fasthttp"}

	for _, v := range javaArr {
		ProcessMap[v] = v
	}

	for _, v := range goArr {
		ProcessMap[v] = v
	}
	for key, values := range mappingRule {
		for _, value := range values {
			//javaArr = append(javaArr, value)
			//goArr = append(goArr, value)
			ProcessMap[value] = key
		}
	}

	// Go
	goTagRegex, _ = regexp.Compile(strings.Join(goArr, "|"))

	// Java
	javaTagRegex, _ = regexp.Compile(strings.Join(javaArr, "|"))

}

func SetMappingTableAppNameDefault(mappingRule string) {
	AppNameDefault = mappingRule
}

type pnameSet map[string]struct{}

func (s pnameSet) Add(v string) {
	s[v] = struct{}{}
}

func ProcessSimpleAllScan() {
	processList, _ := ps.Processes()
	s := &pnameSet{}

	var kthreadPid int32
	for _, p := range processList {
		pName, err := p.Name()
		if err != nil {
			continue
		}

		if pName == "kthreadd" {
			kthreadPid = p.Pid
			break
		}
	}

	for _, p := range processList {
		pName, err := p.Name()
		if err != nil {
			continue
		}

		ppid, err := p.Ppid()
		if err != nil || ppid == kthreadPid {
			continue
		}

		pInfo := &ProcessInfo{}
		pInfo.ProcessName = pName

		if ProcessPostInfo(p.Pid, pName, p, pInfo) != nil {
			pInfo.ProcessType = "UNKNOWN"
			pInfo.AppName = "UNKNOWN"
		}

		if len(pInfo.ProcessName) >= 10 {
			s.Add(fmt.Sprintf("%d&%s", p.Pid, pInfo.ProcessName))
		}
	}

	slice := make([]string, 0, len(*s))

	for k := range *s {
		slice = append(slice, k)
	}
	fmt.Println(strings.Join(slice, "|"))

}

func ProcessAllScan() {
	processList, _ := ps.Processes()
	s := &pnameSet{}

	hostName, err := GetHostName()
	if err != nil {
		hostName = "UNKNOWN"
	}

	for _, p := range processList {

		pName, err := p.Name()
		if err != nil {
			return
		}

		pInfo := &ProcessInfo{}
		pInfo.ProcessName = pName

		if ProcessPostInfo(p.Pid, pName, p, pInfo) != nil {
			pInfo.ProcessType = "UNKNOWN"
			pInfo.AppName = "UNKNOWN"
		}
		if pTagRegex != nil {
			pTagName := pTagRegex.FindString(pInfo.ProcessName)
			if pTagName != "" {
				pInfo.ProcessTagName = pTagName
			} else {
				pInfo.ProcessTagName = pInfo.ProcessName
			}
		} else {
			pInfo.ProcessTagName = pInfo.ProcessName
		}
		if pType, ok := ProcessMap[pInfo.ProcessTagName]; ok {
			pInfo.ProcessType = pType
		} else {
			//if pInfo.ProcessType == ""
			pInfo.ProcessType = pInfo.ProcessTagName
		}

		if processAll == 1 || (whiteList != nil && whiteList.FindString(pInfo.ProcessTagName) != "") {
			if blackList != nil && blackList.FindString(pInfo.ProcessTagName) != "" {
				continue
			}
		} else {
			continue
		}

		pInfo.AppName = findKeyByValues(pInfo.ProcessTagName, pInfo.ProcessType, hostName, "", "")

		if pInfo.AppName == "" {
			//TODO : Default에 따라 정해진 필드보기
			if AppNameDefault == "process_type" {
				pInfo.AppName = pInfo.ProcessType
			} else if AppNameDefault == "host_tag" || AppNameDefault == "" {
				pInfo.AppName = hostName
			} else {
				pInfo.AppName = AppNameDefault
			}
		}

		s.Add(fmt.Sprintf("%s%d", pInfo.ProcessTagName+"&"+pInfo.ProcessType+"&"+pInfo.AppName+"&", p.Pid))
	}

	slice := make([]string, 0, len(*s))

	for k := range *s {
		slice = append(slice, k)
	}
	fmt.Println(strings.Join(slice, "|"))

}

const TEMP_NAME = "Temporary Process"

func ReturnTemporaryProcess(hTag string, k8s bool) *ProcessInfo {
	pInfo := &ProcessInfo{}
	pInfo.ProcessName = TEMP_NAME
	pInfo.ProcessTagName = TEMP_NAME
	pInfo.ProcessType = TEMP_NAME
	pInfo.Pid = -1
	if k8s {
		hTag = fmt.Sprintf("%s[default][node]", hTag)
	}
	if AppNameDefault == "process_type" {
		pInfo.AppName = pInfo.ProcessType
	} else if AppNameDefault == "host_tag" || AppNameDefault == "" {
		pInfo.AppName = hTag
	} else {
		pInfo.AppName = AppNameDefault
	}
	return pInfo
}

func SearchK8SUID(pid int32, logger *logfile.FileLogger) (string, string, error) {
	procDir := os.Getenv("HOST_PROC")

	cgroupPath := filepath.Join(procDir, fmt.Sprintf("%d", pid), "cgroup")
	file, err := os.Open(cgroupPath)
	if err != nil {
		return "", "", err
	}
	defer file.Close()

	scanner := bufio.NewScanner(bufio.NewReader(file))
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Split(line, ":")

		if len(fields) != 3 {
			logger.Println("SearchK8SUID", line)
			continue
		}

		//cgroup path
		cgroupFields := strings.Split(fields[2], "/")
		if len(cgroupFields) != 5 {
			logger.Println("SearchK8SUID", line)
			continue
		}

		//pod group
		podId := ""
		if strings.HasPrefix(cgroupFields[3], "pod") {
			podId = cgroupFields[3]
		} else {
			podFields := strings.Split(cgroupFields[3], "-")
			podId = podFields[len(podFields)-1]
			if strings.HasPrefix(podId, "pod") {
				podId = strings.Replace(podId[3:len(podId)-6], "_", "-", -1)
			} else {
				logger.Println("SearchK8SUID", line)
				continue
			}
		}

		containerFields := strings.Split(cgroupFields[4], "-")
		containerId := containerFields[len(containerFields)-1]
		if len(containerId) > 6 {
			containerId = containerId[:len(containerId)-6]
		} else {
			logger.Println("SearchK8SUID", line)
			continue
		}

		return podId, containerId, nil
	}

	return "", "", nil
}

func ProcessScan(pid int32, hTag, ip, port string, k8s bool, resourceMap *k8s.ResourceMap, logger *logfile.FileLogger) (*ProcessInfo, error) {

	p, err := ps.NewProcess(pid)
	if err != nil {
		return ReturnTemporaryProcess(hTag, k8s), nil
	}

	pName, err := p.Name()
	if err != nil {
		return ReturnTemporaryProcess(hTag, k8s), nil
	}

	// Check
	PMapLock.Lock()
	if pInfo, ok := ProcessInfoMap[pid]; ok && pInfo.ProcessName == pName {
		PMapLock.Unlock()
		return pInfo, nil
	}
	PMapLock.Unlock()

	pInfo := &ProcessInfo{}
	pInfo.Pid = pid
	pInfo.ProcessName = pName

	// pod, container id
	if k8s {
		//search id
		pInfo.PodID, pInfo.ContainerID, _ = SearchK8SUID(pid, logger)
		rInfo := resourceMap.GetPod(pInfo.PodID)
		if rInfo != nil {
			pInfo.PodName = rInfo.ResourceName
			pInfo.Namespace = rInfo.Namespace
			hTag = fmt.Sprintf("%s[%s][pod]", pInfo.PodName, pInfo.Namespace)
		} else {
			hTag = fmt.Sprintf("%s[default][node]", hTag)
		}
		rInfo = resourceMap.GetContainer(pInfo.ContainerID)
		if rInfo != nil {
			pInfo.ContainerName = rInfo.ResourceName
		}
	}

	pInfo.HostName = hTag

	if ProcessPostInfo(pid, pName, p, pInfo) != nil {
		pInfo.ProcessType = "UNKNOWN"
		pInfo.AppName = "UNKNOWN"
	}

	if pTagRegex != nil {
		pTagName := pTagRegex.FindString(pInfo.ProcessName)
		if pTagName != "" {
			pInfo.ProcessTagName = pTagName
		} else {
			pInfo.ProcessTagName = pInfo.ProcessName
		}
	} else {
		pInfo.ProcessTagName = pInfo.ProcessName
	}

	if pType, ok := ProcessMap[pInfo.ProcessTagName]; ok {
		pInfo.ProcessType = pType
		/*
			if aName, ok := AppNameDefaultMap[pType]; ok {
				pInfo.AppName = aName
			}*/
	} else {
		//if pInfo.ProcessType == ""
		pInfo.ProcessType = pInfo.ProcessTagName
		/*
			if aName, ok := AppNameDefaultMap[pInfo.ProcessType]; ok {
				pInfo.AppName = aName
			}*/
	}

	if processAll == 1 || (whiteList != nil && whiteList.FindString(pInfo.ProcessTagName) != "") {
		if blackList != nil && blackList.FindString(pInfo.ProcessTagName) != "" {
			return nil, errors.New(fmt.Sprintf("Check Black List : %s", pInfo.ProcessTagName))
		}
	} else {
		return nil, errors.New(fmt.Sprintf("Check White List : %s", pInfo.ProcessTagName))
	}

	pInfo.AppName = findKeyByValues(pInfo.ProcessTagName, pInfo.ProcessType, hTag, ip, port)

	if pInfo.AppName == "" {
		//TODO : Default에 따라 정해진 필드보기
		if AppNameDefault == "process_type" {
			pInfo.AppName = pInfo.ProcessType
		} else if AppNameDefault == "host_tag" || AppNameDefault == "" {
			pInfo.AppName = hTag
		} else {
			pInfo.AppName = AppNameDefault
		}
	}

	PMapLock.Lock()
	ProcessInfoMap[pid] = pInfo
	PMapLock.Unlock()

	return pInfo, nil
}

func findKeyByValues(processTag, processType, hostName, ip, port string) string {
	for key, appNameKey := range AppNameMap {
		for _, app := range appNameKey {
			if (app.HostName == hostName || app.HostName == "") &&
				(app.ProcessType == processType || app.ProcessType == "") &&
				(app.IP == "" || app.IP == ip) &&
				(app.Port == "" || app.Port == port) &&
				(app.ProcessTag == "" || app.ProcessTag == processTag) {
				return key
			}
		}
	}
	return ""
}

////

var TagRuleFileHash = ""
var TagRuleConfigHash = ""

type TagConfig struct {
	ProcessRegEx     []string                     `yaml:"processRegEx"`
	ProcessWhiteList []string                     `yaml:"processWhiteList"`
	ProcessBlackList []string                     `yaml:"processBlackList"`
	ProcessType      map[string][]string          `yaml:"processType"`
	AppName          map[string][]AppNameKey      `yaml:"appName"`
	AppNameDefault   string                       `yaml:"appNameDefault"`
	UntagOption      map[string]map[string]string `yaml:"untagOption"`
}

func TagRuleFileCheck(path string, reset int) (*TagConfig, error) {
	backFile := path + ".server"
	_, err := os.Stat(backFile)
	if err == nil {
		err = os.Rename(backFile, path)
		if err != nil {
			return nil, err
		}
	}

	hashValue, err := hashutil.GetFileHash(path)
	if TagRuleFileHash == hashValue {
		return nil, nil
	}

	TagRuleFileHash = hashValue

	// YAML 파일 읽기
	yamlFile, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	// YAML 파일 파싱하여 맵으로 저장
	var config TagConfig
	err = yaml.Unmarshal(yamlFile, &config)
	if err != nil {
		return nil, err
	}

	TagRuleSend(&config, reset)
	TagRuleConfigHash = hashutil.GetStructHash(config)

	return &config, nil
}

func TagRuleDecode(path string, pack *pack.ParamPack, logger *logfile.FileLogger) error {
	v := pack.Get("data")

	if v.GetValueType() == value.VALUE_NULL {
		return errors.New("Data Field NULL")
	}

	tagRuleMap := pack.Get("data").(*value.MapValue)
	config := TagConfig{}

	processRegEx := make([]string, 0)
	list := tagRuleMap.Get("processRegEx").(*value.ListValue)
	for i := 0; i < list.Size(); i++ {
		processRegEx = append(processRegEx, list.GetString(i))
	}
	config.ProcessRegEx = processRegEx

	processWhiteList := make([]string, 0)
	list = tagRuleMap.Get("processWhiteList").(*value.ListValue)
	for i := 0; i < list.Size(); i++ {
		processWhiteList = append(processWhiteList, list.GetString(i))
	}
	config.ProcessWhiteList = processWhiteList

	processBlackList := make([]string, 0)
	list = tagRuleMap.Get("processBlackList").(*value.ListValue)
	for i := 0; i < list.Size(); i++ {
		processBlackList = append(processBlackList, list.GetString(i))
	}
	config.ProcessBlackList = processBlackList

	mapValue := tagRuleMap.Get("processType").(*value.MapValue)
	keys := mapValue.Keys()
	pType := make(map[string][]string)
	for keys.HasMoreElements() {
		key := keys.NextString()
		list = mapValue.Get(key).(*value.ListValue)
		processTypeList := make([]string, 0)
		for i := 0; i < list.Size(); i++ {
			processTypeList = append(processTypeList, list.GetString(i))
		}
		pType[key] = processTypeList

	}
	config.ProcessType = pType

	mapValue = tagRuleMap.Get("appName").(*value.MapValue)
	keys = mapValue.Keys()
	appName := make(map[string][]AppNameKey)

	for keys.HasMoreElements() {
		key := keys.NextString()
		hashMap := mapValue.Get(key).(*value.MapValue)

		hashMapKeys := hashMap.Keys()

		appNameList := make([]AppNameKey, 0)
		for hashMapKeys.HasMoreElements() {
			hashKey := hashMapKeys.NextString()
			v := hashMap.Get(hashKey).(*value.MapValue)
			appNameKey := AppNameKey{}
			appNameKey.ProcessTag = v.GetString("processTag")
			appNameKey.ProcessType = v.GetString("processType")
			appNameKey.HostName = v.GetString("hostName")
			appNameKey.IP = v.GetString("ip")
			appNameKey.Port = v.GetString("port")

			appNameList = append(appNameList, appNameKey)

		}
		appName[key] = appNameList
	}

	config.AppName = appName

	config.AppNameDefault = tagRuleMap.GetString("appNameDefault")

	mapValue = tagRuleMap.Get("untagOption").(*value.MapValue)
	keys = mapValue.Keys()
	untagOption := make(map[string]map[string]string)
	for keys.HasMoreElements() {
		ipKey := keys.NextString()
		subMapValue := mapValue.Get(ipKey).(*value.MapValue)
		subKeys := subMapValue.Keys()

		portMap := make(map[string]string)
		for subKeys.HasMoreElements() {
			portKey := subKeys.NextString()
			portMap[portKey] = subMapValue.GetString(portKey)
		}
		untagOption[ipKey] = portMap
	}
	config.UntagOption = untagOption

	// TODO 백업 파일 생성후 메인 로직에서 체크하는 방식, 개선 필요할듯
	hashValue := hashutil.GetStructHash(config)
	if TagRuleConfigHash == hashValue {
		return nil
	}

	yamlData, err := yaml.Marshal(config)
	if err != nil {
		return err
	}

	file, err := os.Create(path + ".server")
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = file.Write(yamlData)
	if err != nil {
		return err
	}

	logger.Println("TagRuleReceive", "TagRuleDecode Complete")
	return nil
}

func TagRuleReceive(path string, logger *logfile.FileLogger) {
	for {
		select {
		case p := <-secure.RecvBuffer: //
			if p.GetPackType() != pack.PACK_PARAMETER {
				continue
			}

			paramPack := p.(*pack.ParamPack)
			if paramPack.Id != PARAM_TAGRULE {
				continue
			}

			err := TagRuleDecode(path, paramPack, logger)
			if err != nil {
				logger.Println("TagRuleDecode", err)
			}

			continue
		case <-time.After(time.Millisecond * 1000):
			time.Sleep(time.Millisecond * 100)
		}
	}
}

func TagRuleSync() {
	secu := secure.GetSecurityMaster()
	if secu.PCODE == 0 || secu.OID == 0 {
		return
	}

	p := pack.NewParamPack()
	p.Id = PARAM_TAGRULE //600
	p.Pcode = secu.PCODE
	p.Oid = secu.OID
	p.Time = dateutil.Now()
	p.PutString("cmd", "get")

	secure.Send(secure.NET_SECURE_HIDE, p, true)
}

func TagRuleSend(config *TagConfig, reset int) {
	secu := secure.GetSecurityMaster()
	if secu.PCODE == 0 || secu.OID == 0 {
		return
	}

	p := pack.NewParamPack()

	v := value.NewMapValue()
	mapValueList := v.NewList("processRegEx")
	for _, configValue := range config.ProcessRegEx {
		mapValueList.AddString(configValue)
	}

	mapValueList = v.NewList("processWhiteList")
	for _, configValue := range config.ProcessWhiteList {
		mapValueList.AddString(configValue)
	}

	mapValueList = v.NewList("processBlackList")
	for _, configValue := range config.ProcessBlackList {
		mapValueList.AddString(configValue)
	}

	subMapValue := value.NewMapValue()
	for configKey, configValues := range config.ProcessType {
		subMapList := subMapValue.NewList(configKey)
		for _, configValue := range configValues {
			subMapList.AddString(configValue)
		}
	}
	v.Put("processType", subMapValue)

	subMapValue = value.NewMapValue()
	for configKey, configValues := range config.AppName {

		keyHashMapValue := value.NewMapValue()

		for _, configValue := range configValues {
			keyMapValue := value.NewMapValue()
			keyMapValue.PutString("processTag", configValue.ProcessTag)
			keyMapValue.PutString("processType", configValue.ProcessType)
			keyMapValue.PutString("hostName", configValue.HostName)
			keyMapValue.PutString("ip", configValue.IP)
			keyMapValue.PutString("port", configValue.Port)

			keyHashMapValue.Put(hashutil.GetStructHash(configValue), keyMapValue)
		}

		subMapValue.Put(configKey, keyHashMapValue)
	}
	v.Put("appName", subMapValue)

	v.PutString("appNameDefault", config.AppNameDefault)

	subMapValue = value.NewMapValue()
	for configKey, configValues := range config.UntagOption {
		subMapValue2 := value.NewMapValue()
		for configKey2, configValue := range configValues {
			subMapValue2.PutString(configKey2, configValue)
		}
		subMapValue.Put(configKey, subMapValue2)
	}
	v.Put("untagOption", subMapValue)

	p.Id = PARAM_TAGRULE //600
	p.Pcode = secu.PCODE
	p.Oid = secu.OID
	p.Time = dateutil.Now()

	if reset == 1 {
		p.PutString("cmd", "put")
	} else {
		p.PutString("cmd", "set")
	}
	p.Put("data", v)

	//서버전송
	secure.Send(secure.NET_SECURE_HIDE, p, true)

}
