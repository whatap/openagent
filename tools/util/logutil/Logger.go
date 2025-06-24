package logutil

import (
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"io/ioutil"
	"path/filepath"
	"runtime/debug"

	"github.com/whatap/golib/lang"
	"github.com/whatap/golib/lang/value"
	"github.com/whatap/golib/util/ansi"
	"github.com/whatap/golib/util/dateutil"
	"github.com/whatap/golib/util/hmap"
)

const (
	LOG_LEVEL_ERROR = 3
	LOG_LEVEL_WARN  = 2
	LOG_LEVEL_INFO  = 1
	LOG_LEVEL_DEBUG = 0
)

type Logger struct {
	lastLog          *hmap.StringLongLinkedMap
	oname            string
	logID            string
	lock             sync.Mutex
	logfile          *os.File
	last             int64
	lastDataUnit     int64
	lastFileRotation bool

	// conf 에 대한 import cycle 에러로 conf에서 설정해 주는 것으로 변경
	confLogInterval        int
	confLogRotationEnabled bool
	confLogKeepDays        int
	//	static PrintWriter pw = null;
	//	static File logfile = null;

	Level int
}

func NewLogger() *Logger {
	//로거인스턴스는 main 함수 실행 전(모듈 import 시점)에 먼저 생성됨
	//log.SetOutput(os.Stdout)
	//log.SetFlags(log.LstdFlags | log.Lshortfile)

	whatapLogger := new(Logger)
	whatapLogger.lastLog = hmap.NewStringLongLinkedMap().SetMax(1000)
	whatapLogger.oname = "boot"
	whatapLogger.logID = "whatap"

	//Default 10초 설정
	whatapLogger.confLogInterval = 10

	//Default true
	whatapLogger.confLogRotationEnabled = true

	//Default 7 일 설정
	whatapLogger.confLogKeepDays = 7

	//Default 로거 레벨 설정
	whatapLogger.Level = LOG_LEVEL_INFO

	// 로거 파일 생성 밍 log에 파일로그 설정.
	go whatapLogger.run()

	return whatapLogger
}

var logger *Logger = NewLogger()

// 패키지 로드
//func init() {
//    logger = NewLogger()
//}

func GetLogger() *Logger {
	if logger != nil {
		return logger
	} else {
		logger = NewLogger()
		return logger
	}
}

// config 에서 설정해 줄 함수
func SetLogInterval(i int) {
	logger.confLogInterval = i
}

// config 에서 설정해 줄 함수
func SetLogRotationEnabled(b bool) {
	logger.confLogRotationEnabled = b
}

// config 에서 설정해 줄 함수
func SetLogKeepDays(i int) {
	logger.confLogKeepDays = i
}
func SetLogID(logID string) {
	logger.logID = logID
}

// logutil.Println 을 동일하게 구현
// 첫번째 인수는 무조건 String으로 ID 값을 넣어야 함( WA111 형식)
// 해당 ID로 중복 확인.
func Println(id string, v ...interface{}) {
	logger.println(id, fmt.Sprint(v...))
}

// logutil.Printf 을 동일하게 구현
// 첫번째 인수는 무조건 String으로 ID 값을 넣어야 함( WA111 형식)
// 해당 ID로 중복 확인.
func Printf(id string, format string, v ...interface{}) {
	logger.println(id, fmt.Sprintf(format, v...))
}

//func Println(id, message string) {
//	logger.println(id, message)
//}

func (this *Logger) println(id, message string) {
	if this.checkOk(id, this.confLogInterval) == false {
		return
	}
	log.Println(logger.build(id, message))
}

func PrintlnError(id, message string, t error) {
	logger.printlnError(id, message, t)
}

func (this *Logger) printlnError(id, message string, t error) {
	if this.checkOk(id, this.confLogInterval) == false {
		return
	}

	log.Println(logger.build(id, message))
}

func Errorln(id string, v ...interface{}) {
	log.Println(ansi.Red(logger.build(id, fmt.Sprint(v...))))
}

func Errorf(id string, format string, v ...interface{}) {
	log.Println(ansi.Red(logger.build(id, fmt.Sprintf(format, v...))))
}

func (this *Logger) build(id, message string) string {
	// runtime.Caller 사용.
	pc, file, line, ok := runtime.Caller(3)
	if !ok {
		return fmt.Sprintf("[%s] %s", id, message) // 호출자 정보를 얻지 못한 경우
	}
	fileParts := strings.Split(file, "/")
	filename := fileParts[len(fileParts)-1]

	funcName := runtime.FuncForPC(pc).Name()
	funcName = filepath.Base(funcName)
	//funcFor := runtime.FuncForPC(pc).Name() // 호출 메소드 이름
	//funcForParts := strings.Split(funcFor, "/")
	//funcName := funcForParts[len(funcForParts)-1]

	// 파일명, 함수명, 라인 번호 포함.
	return fmt.Sprintf("[%s](%s:%d)(%s) %s", id, filename, line, funcName, message)
}

// TODO runtime/debug 에서 현재 시점의 스택 정보를 가져올 수 있지만
// 인수로 맏는 error의 스택은 확인 못함
func GetCallStack() string {
	return logger.getCallStack()
}

func (this *Logger) getCallStack() string {
	defer func() {
		if r := recover(); r != nil {
			log.Println("WA10001 getCallStack Recover", r)
		}
	}()
	return string(debug.Stack())
}

func (this *Logger) checkOk(id string, sec int) bool {
	// TODO 추후 import cycle 오류 조심
	//		if this.conf.IsIgnoreLog(id) {
	//			return false
	//		}

	if sec > 0 {
		last := this.lastLog.Get(id)
		now := dateutil.Now()
		if now < (last + int64(sec)*1000) {
			return false
		}
		this.lastLog.Put(id, now)
	}
	return true
}

func PrintlnStd(msg string, sysout bool) {
	logger.printlnStd(msg, sysout)
}

func (this *Logger) printlnStd(msg string, sysout bool) {
	defer func() {
		if r := recover(); r != nil {
			log.Println("WA10002", "println Recover", r)
		}
	}()
	if sysout {
		fmt.Println(msg)
	} else {
		log.Println(msg)
	}
}

func Update(oname string) {
	logger.update(oname)
}
func (this *Logger) update(oname string) {
	defer func() {
		if r := recover(); r != nil {
			log.Println("WA10003", "Update Recover", r)
		}
	}()

	oname = strings.TrimSpace(oname)
	if oname == this.oname {
		return
	}

	this.oname = oname
	this.openFile()
}

func SetLogSysOut(stdOut bool) {
	logger.SetLogSysOut(stdOut)
}
func (this *Logger) SetLogSysOut(logSysOut bool) {
	if logSysOut {
		multiWriter := io.MultiWriter(this.logfile, os.Stdout)
		log.SetOutput(multiWriter)
	} else {
		log.SetOutput(this.logfile)
	}
}

func (this *Logger) openFile() {
	defer func() {
		if r := recover(); r != nil {
			log.Println("WA10004", "openFile Recover", r)
		}
	}()

	if this.logfile == nil {
		//fmt.Println("Logger open file", "oname=", this.oname, "filname=", fmt.Sprintf("whatap-%s-%s.log", this.oname, dateutil.YYYYMMDD(dateutil.Now())))
		// 로그파일 오픈
		home := GetLogHome()

		if _, err := os.Stat(filepath.Join(home, "logs")); err != nil {
			if os.IsNotExist(err) {
				// file does not exist
				os.Mkdir(filepath.Join(home, "logs"), os.ModePerm)
			} else {
				// other error
			}
		}
		var file *os.File
		var err error
		if this.confLogRotationEnabled {
			file, err = os.OpenFile(filepath.Join(home, "logs", fmt.Sprintf("%s-%s-%s.log", this.logID, this.oname, dateutil.YYYYMMDD(dateutil.Now()))), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
			if err != nil {
				panic(err)
			}
		} else {
			file, err = os.OpenFile(filepath.Join(home, "logs", fmt.Sprintf("%s-%s.log", this.logID, this.oname)), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
			if err != nil {
				panic(err)
			}
		}
		this.logfile = file
		//fmt.Println("Logger open file", this.logfile)

		// 화면출력 동시에 진행할 경우 아래 코드 실행
		multiWriter := io.MultiWriter(this.logfile, os.Stdout)
		log.SetOutput(multiWriter)

		// 표준로거를 파일로그로 변경
		//log.SetOutput(this.logfile)
		log.SetFlags(log.LstdFlags)

		log.Println("")
		log.Println("## OPEN LOG FILE ", this.oname, "", dateutil.TimeStampNow()+" ##")
		log.Println("")
	}

	//defer logfile.Close()
}

func (this *Logger) run() {
	this.last = dateutil.Now()
	this.lastDataUnit = dateutil.GetDateUnitNow()

	this.lastFileRotation = this.confLogRotationEnabled

	for {
		//DEBUG goroutine 로그
		//log.Println("Logger Run")

		this.process()

		time.Sleep(10000 * time.Millisecond)
	}

	//		public void run() {
	//			while (logThread == Thread.currentThread()) {
	//				try {
	//					process();
	//				} catch (Throwable t) {
	//				}
	//				try {
	//					Thread.sleep(10000);
	//				} catch (InterruptedException e) {
	//				}
	//			}
	//		}

}

func (this *Logger) process() {
	this.lock.Lock()
	defer func() {
		this.lock.Unlock()
		if r := recover(); r != nil {
			log.Println("WA10005", " Recover", r) //, string(debug.Stack()))
		}
	}()

	now := dateutil.Now()
	//fmt.Printf("Logger process oname=%s, now=%d \r\n", this.oname, now)

	//if now > this.last+dateutil.MILLIS_PER_HOUR {
	if now > this.last+dateutil.MILLIS_PER_MINUTE {
		this.last = now
		this.clearOldLog()
	}

	if (this.lastFileRotation != this.confLogRotationEnabled) || (this.lastDataUnit != dateutil.GetDateUnitNow()) || (this.logfile == nil) {

		this.logfile.Close()
		this.logfile = nil

		this.lastFileRotation = this.confLogRotationEnabled

		this.lastDataUnit = dateutil.GetDateUnitNow()
	}
	this.openFile()

}

//	static {
//		logThread.setName("WhaTap-Log");
//		logThread.setDaemon(true);
//		logThread.start();
//	}

func (this *Logger) clearOldLog() {
	if this.confLogRotationEnabled == false {
		return
	}
	if this.confLogKeepDays <= 0 {
		return
	}

	//whatapPrefix := "whatap-" + this.oname
	//whatapPrefix := "whatap"
	whatapPrefix := this.logID
	nowUnit := dateutil.GetDateUnitNow()

	home := GetLogHome()
	searchDir := filepath.Join(home, "logs")

	// Get filelist
	files, _ := ioutil.ReadDir(searchDir)

	for _, f := range files {
		if f.IsDir() {
			continue
		}
		name := f.Name()
		// prefix 구분
		//fmt.Printf("file=%s", f.Name())
		if !strings.HasPrefix(name, whatapPrefix+"-") {
			continue
		}
		//
		//		x := strings.LastIndex(name, ".")
		//		if x < 0 {
		//			continue
		//		}
		//
		//		// oname을 구분해서 확인
		//		date := name[len(whatapPrefix)+1 : x]

		// oname 을 구분하지 않고 날짜만 확인 해서 모두 정리
		x := strings.LastIndex(name, ".")
		if x < 0 {
			continue
		}

		s := strings.LastIndex(name, "-")
		//s >= x-1  적어도 한 문자는 slice 되게
		if s < 0 || s >= x-1 {
			continue
		}
		date := name[s+1 : x]

		//fmt.Printf("file=%s, date=%s", f.Name(), date)

		if len(date) != 8 {
			continue
		}

		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Println("WA10006", " File Delete Error", r)
				}
			}()

			d := dateutil.GetYmdTime(date)
			fileUnit := dateutil.GetDateUnit(d)
			if nowUnit-fileUnit > int64(this.confLogKeepDays) {
				//fmt.Println("File Remove", filepath.Join(searchDir,f.Name()))
				err := os.Remove(filepath.Join(searchDir, f.Name()))
				if err != nil {
					log.Println("WA10007", " File Remove Error", err)
				}
			}
		}()
	}
}

func Info(id string, message string) {
	logger.info(id, message)
}

func Infoln(id string, v ...interface{}) {
	logger.info(id, fmt.Sprint(v...))
}
func Infof(id string, format string, v ...interface{}) {
	logger.info(id, fmt.Sprintf(format, v...))
}
func Debug(id string, message string) {
	logger.debug(id, message)
}

func Debugln(id string, v ...interface{}) {
	logger.debug(id, fmt.Sprint(v...))
}
func Debugf(id string, format string, v ...interface{}) {
	logger.debug(id, fmt.Sprintf(format, v...))
}

func (this *Logger) info(id string, message string) {
	message = this.build(id, message)
	this.printlnStd(message, false)
}

func (this *Logger) debug(id string, message string) {
	if this.Level <= LOG_LEVEL_DEBUG {
		message = this.build(id, message)
		this.printlnStd(message, false)
	}
}

func Sysout(message string) {
	logger.sysout(message)
}

func (this *Logger) sysout(message string) {
	fmt.Println(message)
}

// golib/logger/Logger  interface
// Set Level
func SetLevel(lv int) {
	logger.SetLevel(lv)
}
func (this *Logger) SetLevel(lv int) {
	this.Level = lv
}

// Errorf logs an error message, patterned after log.Printf.
func (this *Logger) Errorf(format string, args ...interface{}) {
	Printf("ERROR", format, args...)
}

// Error logs an error message, patterned after log.Print.
func (this *Logger) Error(args ...interface{}) {
	Println("ERROR", args...)
}

// Warnf logs a warning message, patterned after log.Printf.
func (this *Logger) Warnf(format string, args ...interface{}) {
	Printf("WARN", format, args...)
}

// Warn logs a warning message, patterned after log.Print.
func (this *Logger) Warn(args ...interface{}) {
	Println("WARN", args...)
}

// Infof logs an information message, patterned after log.Printf.
func (this *Logger) Infof(format string, args ...interface{}) {
	Printf("INFO", format, args...)
}

// Info logs an information message, patterned after log.Print.
func (this *Logger) Info(args ...interface{}) {
	Println("INFO", args...)
}
func (this *Logger) Infoln(args ...interface{}) {
	Println("INFO", args...)

}

// Debugf logs a debug message, patterned after log.Printf.
func (this *Logger) Debugf(format string, args ...interface{}) {
	Infof("DEBUG", format, args...)
}

// Debug logs a debug message, patterned after log.Print.
func (this *Logger) Debug(args ...interface{}) {
	Infoln("DEBUG", args...)
}

// whatap cache log
func (this *Logger) Printf(id string, format string, args ...interface{}) {
	Printf(id, format, args...)
}
func (this *Logger) Println(id string, args ...interface{}) {
	Println(id, args...)
}

func (this *Logger) GetLogFiles() *value.MapValue {
	out := value.NewMapValue()

	whatapPrefix := this.logID + "-" + this.oname
	home := GetLogHome()
	searchDir := filepath.Join(home, "logs")

	// Get filelist
	files, _ := ioutil.ReadDir(searchDir)

	for _, f := range files {
		if f.IsDir() {
			continue
		}
		name := f.Name()

		x := strings.Index(name, ".")
		if x < 0 {
			continue
		}
		if name != "whatap-hook.log" {
			if !strings.HasPrefix(name, whatapPrefix+"-") {
				continue
			}
			date := name[len(whatapPrefix)+1 : x]

			if len(date) != 8 {
				continue
			}
		}

		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Println("WA10008", " File Delete Error", r)
				}
			}()
			out.Put(f.Name(), value.NewDecimalValue(f.Size()))
		}()

		if out.Size() >= 100 {
			break
		}
	}

	searchDotnetPath := filepath.Join(os.Getenv("ProgramData"), "WhaTap", "dotnet-profiler.log")
	fi, err := os.Stat(searchDotnetPath)
	if err == nil {
		out.Put(fi.Name(), value.NewDecimalValue(fi.Size()))
	}

	return out
}

func (this *Logger) Read(file string, endpos int64, length int64) *LogData {
	var ret string

	if file == "" || length == 0 {
		return nil
	}

	if file == "dotnet-profiler.log" {
	} else {
		if file != "whatap-hook.log" {
			whatapPrefix := this.logID + "-" + this.oname
			if !strings.HasPrefix(file, whatapPrefix) {
				return nil
			}
		}
	}

	// 로그파일 오픈 -> 추후 logutil.Logger로 옮길 예정.
	// 폴더 없을 때 발생하는 오류를 임시로 조정.
	// logs 폴더 없는 경우 생성
	if _, err := os.Stat(filepath.Join(GetLogHome(), "logs")); err != nil {
		if os.IsNotExist(err) {
			// file does not exist
			os.Mkdir(filepath.Join(GetLogHome(), "logs"), os.ModePerm)
		} else {
			// other error
		}
	}

	searchFilePath := filepath.Join(GetLogHome(), "logs", file)

	if file == "dotnet-profiler.log" {
		searchFilePath = filepath.Join(os.Getenv("ProgramData"), "WhaTap", file)
	}

	f, err := os.Open(searchFilePath)
	if err != nil {
		return nil
	}

	fInfo, err := f.Stat()
	if fInfo.Size() < endpos {
		return nil
	}

	if endpos < 0 {
		endpos = fInfo.Size()
	}
	start := int64(math.Max(0, float64(endpos-length)))

	available := fInfo.Size() - start
	readable := int(math.Min(float64(available), float64(length)))
	//readable = int(math.Min(math.MinInt16, float64(readable)))

	buff := make([]byte, readable)

	n, err := f.ReadAt(buff, start)

	//log.Println("Logger Read ", "file=", file, ",size=", fInfo.Size(), "readable=", readable, ",endpos=", endpos, ",start=", start, ",length=", length, "read=", n) //, ",result=" + string(buff));

	if err != nil {
		log.Println("WA1000901", " Read Error ", err)
		return nil
	}
	ret = string(buff)

	next := start + int64(n)

	if (next + length) > fInfo.Size() {
		next = -1
	} else {
		next += length
	}

	defer func() {
		f.Close()
		if r := recover(); r != nil {
			log.Println("WA10009", " Read Recover", r)
			ret = ""
		}
	}()

	//return ret
	return NewLogData(start, next, ret)

}

type FileLog struct { //implements IClose {
	//private PrintWriter out;
	out *os.File
}

func NewFileLog(filename string) *FileLog {
	defer func() {
		if r := recover(); r != nil {
			log.Println("WA100010", " FileLog New Recover", r)
		}
	}()
	p := new(FileLog)

	var err error
	p.out, err = os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		log.Println("WA100011", " Open File Error", err)
		p.out = nil
	}

	return p
}

func (this *FileLog) Println(message string) {
	if this.out == nil {
		return
	}

	this.out.WriteString(fmt.Sprintf("%d %s", dateutil.Now(), message))
}

func (this *FileLog) Close() {
	this.out.Close()
}

func GetLogHome() string {
	home := os.Getenv("WHATAP_HOME") //os.GetEnv("whatap.home")
	if home == "" {
		home = "."
	}

	aType := os.Getenv("WHATAP_APP_TYPE")
	if aType != "" {
		if v, err := strconv.Atoi(aType); err == nil {
			at := int16(v)
			if at == lang.APP_TYPE_DOTNET || at == lang.APP_TYPE_BSM_DOTNET {
				dotnet_home := os.Getenv("WHATAP_DOTNET_HOME")
				if dotnet_home != "" {
					home = dotnet_home
				}
			}
		}
	}
	return home
}

type LogData struct {
	Before int64
	Next   int64
	Text   string
}

func NewLogData(pre, next int64, text string) *LogData {
	p := new(LogData)
	p.Before = pre
	p.Next = next
	p.Text = text

	return p
}

func LoggerMain() {
	name := "whatap-19701123.log"
	x := strings.Index(name, ".")
	date := name[len("whatap-"):x]
	fmt.Println(date)
}
