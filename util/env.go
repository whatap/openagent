package util

import (
	"os"

	"github.com/whatap/golib/config"
	"github.com/whatap/golib/logger/logfile"
)

type Env struct {
	k8s          bool
	hostName     string
	publicIP     string
	intOption    map[string]int32
	stringOption map[string]string
	pcode        int32
	npmHome      string
	config       config.Config
	logger       *logfile.FileLogger
}

func InitEnv(enable bool, ip string, config config.Config) *Env {
	env := &Env{}
	env.SetK8S(enable)
	env.SetHostName()
	env.publicIP = ip
	env.intOption = make(map[string]int32)
	env.stringOption = make(map[string]string)
	env.config = config

	return env
}

func (env *Env) SetLogger(logger *logfile.FileLogger) {
	env.logger = logger
}

func (env *Env) SetK8S(enable bool) {
	env.k8s = enable
}

func (env *Env) SetHostName() {
	env.hostName, _ = os.Hostname()
}

func (env *Env) SetPublicIP(ip string) {
	env.publicIP = ip
}

func (env *Env) SetIntOption(key string, value int32) {
	env.intOption[key] = value
}

func (env *Env) SetStringOption(key string, value string) {
	env.stringOption[key] = value
}

func (env *Env) SetPcode(pcode int32) {
	env.pcode = pcode
}

func (env *Env) SetNpmHome(path string) {
	env.npmHome = path
}

func (env *Env) GetLogger() *logfile.FileLogger {
	return env.logger
}

func (env *Env) GetIsK8S() bool {
	return env.k8s
}

func (env *Env) GetHostName() string {
	return env.hostName
}

func (env *Env) GetPublicIP() string {
	return env.publicIP
}

func (env *Env) GetIntOption(key string) int32 {
	return env.intOption[key]
}

func (env *Env) GetStringOption(key string) string {
	return env.stringOption[key]
}

func (env *Env) GetPcode() int32 {
	return env.pcode
}

func (env *Env) GetNpmHome() string {
	return env.npmHome
}

func (env *Env) GetConfig() config.Config {
	return env.config
}

func (env *Env) GetTraceOpt() (int, int, int, int, int, int, int, int, int, *logfile.FileLogger) {
	onoff := int(env.config.GetInt("traceRoute", 0))
	topN := int(env.config.GetInt("traceTopN", 5))
	maxHop := int(env.config.GetInt("traceMaxHop", 30))
	measurement := int(env.config.GetInt("traceMeasurementCount", 3))
	synPort := int(env.config.GetInt("traceTcpSynPort", -1))
	timeout := int(env.config.GetInt("traceTimeout", -1))
	parallel := int(env.config.GetInt("traceParallelCount", 1))
	channelSize := int(env.config.GetInt("traceChannelSize", 1000))
	channelLimitPercent := int(env.config.GetInt("traceChannelLimitPercent", 70))
	logger := env.GetLogger()

	return onoff, topN, maxHop, measurement, synPort, timeout, parallel, channelSize, channelLimitPercent, logger
}
