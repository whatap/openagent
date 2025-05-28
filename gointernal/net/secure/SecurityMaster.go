package secure

import (
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/whatap/golib/io"
	"github.com/whatap/golib/util/cmdutil"
	"github.com/whatap/golib/util/hash"
	"github.com/whatap/golib/util/iputil"

	"github.com/whatap/gointernal/lang/license"
	"github.com/whatap/gointernal/util/crypto"
	"github.com/whatap/gointernal/util/oidutil"
)

type SecurityMaster struct {
	PCODE       int64
	OID         int32
	ONAME       string
	IP          int32
	SECURE_KEY  []byte
	Cypher      *crypto.Cypher
	lastOidSent int64
	PUBLIC_IP   int32
	lastLicense string
	lastOid     int64
}

type SecuritySession struct {
	TRANSFER_KEY int32
	SECURE_KEY   []byte
	HIDE_KEY     int32
	Cypher       *crypto.Cypher
}

var master *SecurityMaster = nil
var secSession *SecuritySession = nil
var mutex = sync.Mutex{}

func NewSecurityMaster() *SecurityMaster {
	p := new(SecurityMaster)
	p.update()
	//langconf.AddConfObserver("SecurityMaster", p)
	return p
}

func GetSecurityMaster() *SecurityMaster {
	if master != nil {
		return master
	}
	mutex.Lock()
	defer mutex.Unlock()

	if master != nil {
		return master
	}
	master = NewSecurityMaster()

	return master
}
func GetSecuritySession() *SecuritySession {
	if secSession != nil {
		return secSession
	}
	mutex.Lock()
	defer mutex.Unlock()
	if secSession != nil {
		return secSession
	}
	secSession = &SecuritySession{}
	return secSession
}
func UpdateNetCypherKey(data []byte) {
	if conf.CypherLevel > 0 {
		data = GetSecurityMaster().Cypher.Decrypt(data)
	}
	in := io.NewDataInputX(data)
	secSession.TRANSFER_KEY = in.ReadInt()
	secSession.SECURE_KEY = in.ReadBlob()
	secSession.HIDE_KEY = in.ReadInt()
	secSession.Cypher = crypto.NewCypher(secSession.SECURE_KEY, secSession.HIDE_KEY)
	master.PUBLIC_IP = in.ReadInt()
}

func (this *SecurityMaster) Run() {
	this.update()
}

func (this *SecurityMaster) update() {
	defer func() {
		if r := recover(); r != nil {
			conf.Log.Println("WA10801", " Recover", r)
		}
	}()
	conf.Log.Println(">>>>", ",cypher=", this.Cypher, ", license=", conf.License)
	if this.Cypher == nil || conf.License != this.lastLicense {
		this.lastLicense = conf.License
		this.resetLicense(conf.License)
	}
}

func (this *SecurityMaster) DecideAgentOnameOid(myIp string) {
	this.AutoAgentNameOrPattern(myIp)

	ip := io.ToInt(iputil.ToBytes(myIp), 0)

	oidutil.SetIp(os.Getenv("whatap.ip"))
	oidutil.SetPort(os.Getenv("whatap.port"))
	oidutil.SetHostName(os.Getenv("whatap.hostname"))
	oidutil.SetType(os.Getenv("whatap.type"))
	oidutil.SetProcess(os.Getenv("whatap.process"))
	//docker full id
	oidutil.SetDocker(os.Getenv("whatap.docker"))
	oidutil.SetIps(os.Getenv("whatap.ips"))
	oname := oidutil.MakeOname(os.Getenv("whatap.name"))

	this.IP = ip
	this.ONAME = oname
	this.OID = hash.HashStr(oname)
	conf.Oid = this.OID

	os.Setenv("whatap.oid", strconv.Itoa(int(this.OID)))
	os.Setenv("whatap.oname", this.ONAME)
	if this.lastOid != int64(this.OID) {
		this.lastOid = int64(this.OID)

	}
	props := map[string]string{}
	props["PCODE"] = fmt.Sprint(this.PCODE)
	props["OID"] = fmt.Sprint(this.OID)
	// from config
	if conf.Config != nil {
		conf.Config.SetValues(&props)
	}

	//fmt.Println("PCODE=", this.PCODE, "OID=", this.OID, "ONAME=", this.ONAME)
	//logutil.Println("WA10802"," PCODE=", this.PCODE, "OID=", this.OID, "ONAME=", this.ONAME)
}

func (this *SecurityMaster) AutoAgentNameOrPattern(myIp string) {
	os.Setenv("whatap.ip", myIp)
	os.Setenv("whatap.port", "")
	hostName, _ := os.Hostname()
	os.Setenv("whatap.hostname", hostName)
	// from config
	os.Setenv("whatap.type", conf.AppName)
	os.Setenv("whatap.process", conf.AppProcessName)
	//docker full id
	os.Setenv("whatap.docker", cmdutil.GetDockerFullId())
	os.Setenv("whatap.ips", iputil.GetIPsToString())

	// from config
	os.Setenv("whatap.name", conf.ObjectName)
}

func (this *SecurityMaster) resetLicense(lic string) {
	pcode, security_key := license.Parse(lic)
	this.PCODE = pcode
	conf.Pcode = pcode
	this.SECURE_KEY = security_key
	this.Cypher = crypto.NewCypher(this.SECURE_KEY, 0)
}

func (this *SecurityMaster) WaitForInit() {
	for this.Cypher == nil {
		time.Sleep(1000 * time.Millisecond)
	}
}

func (this *SecurityMaster) WaitForInitFor(timeoutSec float64) {
	started := time.Now()
	for this.Cypher == nil && time.Now().Sub(started).Seconds() < timeoutSec {
		time.Sleep(1000 * time.Millisecond)
	}
}
