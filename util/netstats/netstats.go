package netstats

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/cakturk/go-netstat/netstat"
	"github.com/containerd/containerd"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/shirou/gopsutil/net"
)

// //////////////////////////////////////////////////////////////////////////////////////////////
// netstats 정보 획득 로직 + Process

type establishKey struct {
	localIP     string
	foreignIP   string
	foreignPort uint32
}

type listenKey struct {
	localIP   string
	localPort uint32
}

type NetstatsInfo struct {
	tcpEstablishMap map[establishKey]int32
	tcpListenMap    map[listenKey]int32
	udpEstablishMap map[establishKey]int32
	udpListenMap    map[listenKey]int32

	localIPList map[string]bool
	listenPort  map[uint32]bool
}

type Netstats struct {
	netstatsInfo []*NetstatsInfo
	index        int
}

func NewNetstats() *Netstats {
	netstats := &Netstats{}

	netstats.netstatsInfo = make([]*NetstatsInfo, 2)
	netstats.netstatsInfo[0] = NewNetstatsInfo()
	netstats.index = 0

	return netstats
}

func (netstats *Netstats) ReloadNetstats() {
	nextIndex := netstats.index ^ 1

	//TODO lock 범위 검토
	//과거 포인터는 알아서 GC가 처리
	netstats.netstatsInfo[nextIndex] = NewNetstatsInfo()

	netstats.index = nextIndex
}

func (netstats *Netstats) CheckDirectoin(localPort uint32) string {
	netstatsInfo := netstats.netstatsInfo[netstats.index]

	if netstatsInfo.ListenPortCheck(localPort) {
		return "IN"
	}
	return "OUT"

}

func (netstats *Netstats) IsListenPort(port uint32) bool {
	netstatsInfo := netstats.netstatsInfo[netstats.index]

	return netstatsInfo.ListenPortCheck(port)
}

func (netstats *Netstats) IsLocalIP(ip string) bool {
	netstatsInfo := netstats.netstatsInfo[netstats.index]

	if netstatsInfo.LocalIPCheck(ip) {
		return true
	}
	return false
}

func (netstats *Netstats) SearchProcessInfo(localIP, foreignIP string, localPort, foreignPort uint16, protocol string) int32 {
	nowIndex := netstats.index

	netstatsInfo := netstats.netstatsInfo[nowIndex]
	var pid int32

	if protocol == "TCP" {
		pid = netstatsInfo.TcpListenCheck(localIP, localPort)
		if pid == -1 {
			pid = netstatsInfo.TcpEstablishCheck(localIP, foreignIP, foreignPort)
		}
	} else if protocol == "UDP" {
		pid = netstatsInfo.UdpListenCheck(localIP, localPort)
		if pid == -1 {
			pid = netstatsInfo.UdpEstablishCheck(localIP, foreignIP, foreignPort)
		}
	}

	return pid
}

func NewNetstatsInfo() *NetstatsInfo {
	netstatsInfo := &NetstatsInfo{}
	netstatsInfo.tcpEstablishMap = make(map[establishKey]int32)
	netstatsInfo.tcpListenMap = make(map[listenKey]int32)
	netstatsInfo.udpEstablishMap = make(map[establishKey]int32)
	netstatsInfo.udpListenMap = make(map[listenKey]int32)
	netstatsInfo.localIPList = make(map[string]bool)
	netstatsInfo.listenPort = make(map[uint32]bool, 0)

	if runtime.GOOS == "windows" {
		netstatsInfo.stats_win()
	} else {
		netstatsInfo.tcpStats()
		netstatsInfo.udpStats()
	}

	// 로컬 IP 주소 가져오기
	ifaces, err := net.Interfaces()
	if err != nil {
		fmt.Print(err)
		return nil
	}
	for _, i := range ifaces {
		addrs := i.Addrs
		if err != nil {
			fmt.Print(err)
			return nil
		}
		for _, addr := range addrs {
			localIP := strings.Split(addr.Addr, "/")[0]

			netstatsInfo.localIPList[localIP] = true
		}
	}

	return netstatsInfo
}

func getTCPConnections() ([]net.ConnectionStat, error) {
	return net.Connections("tcp")
}

func getUDPConnections() ([]net.ConnectionStat, error) {
	return net.Connections("udp")
}

func convertAddr(str string) (string, int) {
	index := strings.LastIndex(str, ":")
	if index == -1 {
		return "", -1
	}

	ip := str[:index]
	ip = strings.Trim(ip, "[]")
	strip := strings.Split(ip, "%")
	ip = strip[0]

	portStr := str[index+1:]
	port, _ := strconv.Atoi(portStr)

	return ip, port
}

func (netstatsInfo *NetstatsInfo) stats_win() {
	out, err := exec.Command("netstat", "-ano").Output()
	if err != nil {
		fmt.Println(err)
		return
	}

	buf := bytes.NewReader(out)
	reader := bufio.NewReader(buf)
	for {
		lineBytes, _, err := reader.ReadLine()
		if err != nil {
			break
		}

		fields := strings.Fields(string(lineBytes))
		if len(fields) < 4 {
			continue
		}

		localIP, localPort := convertAddr(fields[1])
		if localPort == -1 {
			continue
		}

		foreignIP, foreignPort := convertAddr(fields[2])
		if foreignPort == -1 {
			continue
		}

		if fields[0] == "TCP" {
			state := fields[3]
			pidStr := fields[4]

			pid, _ := strconv.Atoi(pidStr)
			if pid == 0 {
				continue
			}

			/*
				if state == "ESTABLISHED" || state == "CLOSE_WAIT" {
					key := establishKey{}
					key.localIP = localIP
					key.foreignIP = foreignIP
					key.foreignPort = uint32(foreignPort)

					netstatsInfo.tcpEstablishMap[key] = int32(pid)
					netstatsInfo.localIPList[key.localIP] = true
				} else */

			if state == "LISTENING" || state == "LISTEN" {
				key := listenKey{}
				key.localIP = localIP
				key.localPort = uint32(localPort)

				netstatsInfo.tcpListenMap[key] = int32(pid)
				netstatsInfo.localIPList[key.localIP] = true
				netstatsInfo.listenPort[key.localPort] = true
			} else {
				key := establishKey{}
				key.localIP = localIP
				key.foreignIP = foreignIP
				key.foreignPort = uint32(foreignPort)

				netstatsInfo.tcpEstablishMap[key] = int32(pid)
				netstatsInfo.localIPList[key.localIP] = true

			}
		} else if fields[0] == "UDP" {
			pidStr := fields[3]

			pid, _ := strconv.Atoi(pidStr)
			if foreignPort != 0 {
				key := establishKey{}
				key.localIP = localIP
				key.foreignIP = foreignIP
				key.foreignPort = uint32(foreignPort)

				netstatsInfo.udpEstablishMap[key] = int32(pid)
				netstatsInfo.localIPList[key.localIP] = true
			} else {
				key := listenKey{}
				key.localIP = localIP
				key.localPort = uint32(localPort)

				netstatsInfo.udpListenMap[key] = int32(pid)
				netstatsInfo.localIPList[key.localIP] = true
				netstatsInfo.listenPort[key.localPort] = true
			}
		}
	}
	return

}
func (netstatsInfo *NetstatsInfo) tcpStats() {
	tcpConnections, err := getTCPConnections()
	if err != nil {
		log.Fatal(err)
	}

	for _, conn := range tcpConnections {
		if conn.Status == "ESTABLISHED" || conn.Status == "CLOSE_WAIT" {
			key := establishKey{}
			key.localIP = conn.Laddr.IP
			key.foreignIP = conn.Raddr.IP
			key.foreignPort = conn.Raddr.Port

			netstatsInfo.tcpEstablishMap[key] = int32(conn.Pid)
			netstatsInfo.localIPList[key.localIP] = true
		} else if conn.Status == "LISTEN" {
			key := listenKey{}
			key.localIP = conn.Laddr.IP
			key.localPort = conn.Laddr.Port

			netstatsInfo.tcpListenMap[key] = int32(conn.Pid)
			netstatsInfo.localIPList[key.localIP] = true
			netstatsInfo.listenPort[key.localPort] = true
		}
	}
}

func (netstatsInfo *NetstatsInfo) udpStats() {
	udpConnections, err := getUDPConnections()
	if err != nil {
		log.Fatal(err)
	}

	for _, conn := range udpConnections {
		if conn.Raddr.Port != 0 { //ESTABLISHED
			key := establishKey{}
			key.localIP = conn.Laddr.IP
			key.foreignIP = conn.Raddr.IP
			key.foreignPort = conn.Raddr.Port

			netstatsInfo.udpEstablishMap[key] = int32(conn.Pid)
			netstatsInfo.localIPList[key.localIP] = true
		} else {
			key := listenKey{}
			key.localIP = conn.Laddr.IP
			key.localPort = conn.Laddr.Port

			netstatsInfo.udpListenMap[key] = int32(conn.Pid)
			netstatsInfo.localIPList[key.localIP] = true
			netstatsInfo.listenPort[key.localPort] = true
		}
	}
}
func (netstatsInfo *NetstatsInfo) UdpEstablishCheck(localIP, foreignIP string, foreignPort uint16) int32 {
	key := establishKey{}
	key.localIP = localIP
	key.foreignIP = foreignIP
	key.foreignPort = uint32(foreignPort)

	if pid, ok := netstatsInfo.udpEstablishMap[key]; ok {
		return pid
	}

	key.localIP = "0.0.0.0"
	if pid, ok := netstatsInfo.udpEstablishMap[key]; ok {
		return pid
	}

	key.localIP = "::"
	if pid, ok := netstatsInfo.udpEstablishMap[key]; ok {
		return pid
	}

	return -1
}

func (netstatsInfo *NetstatsInfo) UdpListenCheck(ip string, port uint16) int32 {
	key := listenKey{}
	key.localIP = ip
	key.localPort = uint32(port)

	if pid, ok := netstatsInfo.udpListenMap[key]; ok {
		return pid
	}

	key.localIP = "0.0.0.0"
	if pid, ok := netstatsInfo.udpListenMap[key]; ok {
		return pid
	}

	key.localIP = "::"
	if pid, ok := netstatsInfo.udpListenMap[key]; ok {
		return pid
	}
	return -1
}
func (netstatsInfo *NetstatsInfo) TcpEstablishCheck(localIP, foreignIP string, foreignPort uint16) int32 {
	key := establishKey{}
	key.localIP = localIP
	key.foreignIP = foreignIP
	key.foreignPort = uint32(foreignPort)

	if pid, ok := netstatsInfo.tcpEstablishMap[key]; ok {
		return pid
	}

	key.localIP = "0.0.0.0"
	if pid, ok := netstatsInfo.tcpEstablishMap[key]; ok {
		return pid
	}

	key.localIP = "::"
	if pid, ok := netstatsInfo.tcpEstablishMap[key]; ok {
		return pid
	}

	return -1
}

func (netstatsInfo *NetstatsInfo) TcpListenCheck(ip string, port uint16) int32 {
	key := listenKey{}
	key.localIP = ip
	key.localPort = uint32(port)

	if pid, ok := netstatsInfo.tcpListenMap[key]; ok {
		return pid
	}

	key.localIP = "0.0.0.0"
	if pid, ok := netstatsInfo.tcpListenMap[key]; ok {
		return pid
	}

	key.localIP = "::"
	if pid, ok := netstatsInfo.tcpListenMap[key]; ok {
		return pid
	}

	return -1
}

func (netstatsInfo *NetstatsInfo) LocalIPCheck(ip string) bool {
	if _, ok := netstatsInfo.localIPList[ip]; ok {
		return true
	}
	return false
}

func (netstatsInfo *NetstatsInfo) ListenPortCheck(port uint32) bool {
	if _, ok := netstatsInfo.listenPort[port]; ok {
		return true
	}
	return false
}

/////////////////////////////////////////////////////////////////////////////

// FOR BPF
// 별도로 작성된 프로젝트가 합쳦비면서 여러 라이브러리조합이 짬뽕됨
// 추후 하나로 통일 및 코드 정리 필요

/////////////////////////////////////////////////////////////////////////////

type Set map[uint16]netstat.SkState

func (s Set) Add(v uint16, state netstat.SkState) {
	if old, ok := s[v]; ok {
		if old != netstat.Listen {
			s[v] = state
		}
	} else {
		s[v] = state
	}
}

/*
	func allProcessPortScan(s *Set) {
		processes, err := ps.Processes()
		if err != nil {
			return
		}

		for _, process := range processes {
			fmt.Printf("PID: %d\n", process.Pid())
			file := fmt.Sprintf("/proc/%d/net/tcp", process.Pid())
			data, err := ioutil.ReadFile(file)
			if err != nil {
				continue
			}
			lines := strings.Split(string(data), "\n")
			for _, line := range lines {
				if line == "" {
					continue
				}

				fields := strings.Fields(line)

				if len(fields) < 2 || fields[0] == "sl" {
					continue
				}

				if fields[3] != "0A" {
					continue
				}

				localAddress := fields[1]

				parts := strings.Split(localAddress, ":")

				if len(parts) < 2 {
					continue
				}

				portHex := parts[1]

				port, err := strconv.ParseInt(portHex, 16, 0)

				if err != nil {
					continue
				}

				s.Add(uint16(port))

			}
		}
		fmt.Println(s)

}
*/

func hostPortScan(tcpSet *Set, udpSet *Set) {
	fn := func(s *netstat.SockTabEntry) bool {
		if s.State == netstat.Listen || s.State == netstat.Established {
			return true
		} else {
			return false
		}
	}

	tabs, err := netstat.TCPSocks(fn)

	if err == nil {
		for _, tab := range tabs {
			tcpSet.Add(tab.LocalAddr.Port, tab.State)
		}
	}

	tabs, err = netstat.TCP6Socks(fn)

	if err == nil {
		for _, tab := range tabs {
			tcpSet.Add(tab.LocalAddr.Port, tab.State)
		}
	}

	tabs, err = netstat.UDPSocks(fn)

	if err == nil {
		for _, tab := range tabs {
			udpSet.Add(tab.LocalAddr.Port, tab.State)
		}
	}

	tabs, err = netstat.UDP6Socks(fn)

	if err == nil {
		for _, tab := range tabs {
			udpSet.Add(tab.LocalAddr.Port, tab.State)
		}
	}
}

func checkDockerRunning(tcpSet *Set, udpSet *Set) bool {
	timeout := 10 * time.Millisecond
	cli, err := client.NewClientWithOpts(client.WithTimeout(timeout))
	if err != nil {
		return false
	}

	cli.NegotiateAPIVersion(context.Background())

	_, err = cli.Ping(context.Background())

	if err != nil {
		return false
	}

	containers, err := cli.ContainerList(context.Background(), types.ContainerListOptions{})
	if err != nil {
		return false
	}

	for _, container := range containers {
		execConfig := types.ExecConfig{
			Cmd:          []string{"cat", "/proc/net/tcp", "/proc/net/tcp6"},
			Tty:          false,
			AttachStdout: true,
			AttachStderr: true,
		}

		execResponse, err := cli.ContainerExecCreate(context.Background(), container.ID, execConfig)
		if err != nil {
			continue
		}

		attachResponse, err := cli.ContainerExecAttach(context.Background(), execResponse.ID, types.ExecStartCheck{})
		if err != nil {
			continue
		}

		defer attachResponse.Close()

		listen_ports, established_ports := parseProcNetTCP(attachResponse.Reader)

		for _, port := range listen_ports {
			tcpSet.Add(uint16(port), netstat.Listen)
		}

		for _, port := range established_ports {
			tcpSet.Add(uint16(port), netstat.Established)
		}

		execConfig = types.ExecConfig{
			Cmd:          []string{"cat", "/proc/net/udp", "/proc/net/udp6"},
			Tty:          false,
			AttachStdout: true,
			AttachStderr: true,
		}

		execResponse, err = cli.ContainerExecCreate(context.Background(), container.ID, execConfig)
		if err != nil {
			continue
		}

		attachResponse, err = cli.ContainerExecAttach(context.Background(), execResponse.ID, types.ExecStartCheck{})
		if err != nil {
			continue
		}

		defer attachResponse.Close()

		ports := parseProcNetUDP(attachResponse.Reader)

		for _, port := range ports {
			udpSet.Add(uint16(port), netstat.Listen)
		}

	}
	return true

}

func parseProcNetUDP(reader io.Reader) []int {
	scanner := bufio.NewScanner(reader)

	var ports []int

	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)

		if len(fields) < 3 || fields[0] == "s1" {
			continue
		}

		foreignAddress := fields[2]
		parts := strings.Split(foreignAddress, ":")

		if len(parts) < 2 {
			continue
		}

		foreignIP, err := strconv.ParseInt(parts[0], 16, 0)
		if err != nil {
			continue
		}

		foreignPort, err := strconv.ParseInt(parts[1], 16, 0)
		if err != nil {
			continue
		}

		if foreignIP != 0 || foreignPort != 0 {
			continue
		}

		localAddress := fields[1]
		parts = strings.Split(localAddress, ":")

		/*
			localIP, err := strconv.ParseInt(parts[0], 16, 0)
			if err != nil {
				continue
			}
		*/
		localPort, err := strconv.ParseInt(parts[1], 16, 0)
		if err != nil {
			continue
		}

		ports = append(ports, int(localPort))

	}
	return ports
}

func parseProcNetTCP(reader io.Reader) ([]int, []int) {
	scanner := bufio.NewScanner(reader)

	var listen_ports []int
	var established_ports []int

	for scanner.Scan() {
		line := scanner.Text()

		fields := strings.Fields(line)

		if len(fields) < 4 || fields[0] == "sl" {
			continue
		}

		if fields[3] != "0A" && fields[3] != "01" {
			continue
		}

		localAddress := fields[1]

		parts := strings.Split(localAddress, ":")

		if len(parts) < 2 {
			continue
		}

		portHex := parts[1]

		port, err := strconv.ParseInt(portHex, 16, 0)

		if err != nil {
			continue
		}

		if fields[3] == "0A" {
			listen_ports = append(listen_ports, int(port))
		} else if fields[3] == "01" {
			established_ports = append(established_ports, int(port))
		}
	}

	return listen_ports, established_ports
}

func checkContainerdRunning() bool {
	timeout := 10 * time.Millisecond
	clientOpts := []containerd.ClientOpt{containerd.WithTimeout(timeout)}
	client, err := containerd.New("/run/containerd/containerd.sock", clientOpts...)
	if err != nil {
		return false
	}
	defer client.Close()

	return true
}

func containerPortScan(tcpSet *Set, udpSet *Set) {
	if checkDockerRunning(tcpSet, udpSet) {
	} else if checkContainerdRunning() {
		//getContainerdListeningPorts()
	}
}

func k8sPortScan(tcpSet *Set, udpSet *Set) {
	procDir := os.Getenv("HOST_PROC")

	pids, err := ioutil.ReadDir(procDir)
	if err != nil {
		return
	}

	for _, pid := range pids {
		if pid.IsDir() {
			pidPath := filepath.Join(procDir, pid.Name())

			tcpFile := filepath.Join(pidPath, "net/tcp")
			if _, err := os.Stat(tcpFile); err == nil {
				file, err := os.Open(tcpFile)
				if err != nil {
					continue
				}
				defer file.Close()

				listen_ports, established_ports := parseProcNetTCP(bufio.NewReader(file))
				for _, port := range listen_ports {
					tcpSet.Add(uint16(port), netstat.Listen)
				}

				for _, port := range established_ports {
					tcpSet.Add(uint16(port), netstat.Established)
				}

			}

			tcp6File := filepath.Join(pidPath, "net/tcp6")
			if _, err := os.Stat(tcp6File); err == nil {
				file, err := os.Open(tcp6File)
				if err != nil {
					continue
				}
				defer file.Close()

				listen_ports, established_ports := parseProcNetTCP(bufio.NewReader(file))
				for _, port := range listen_ports {
					tcpSet.Add(uint16(port), netstat.Listen)
				}

				for _, port := range established_ports {
					tcpSet.Add(uint16(port), netstat.Established)
				}
			}

			udpFile := filepath.Join(pidPath, "net/udp")
			if _, err := os.Stat(udpFile); err == nil {
				file, err := os.Open(udpFile)
				if err != nil {
					continue
				}
				defer file.Close()

				ports := parseProcNetUDP(bufio.NewReader(file))
				for _, port := range ports {
					udpSet.Add(uint16(port), netstat.Listen)
				}
			}
			udp6File := filepath.Join(pidPath, "net/udp6")
			if _, err := os.Stat(udp6File); err == nil {
				file, err := os.Open(udp6File)
				if err != nil {
					continue
				}
				defer file.Close()

				ports := parseProcNetUDP(bufio.NewReader(file))
				for _, port := range ports {
					udpSet.Add(uint16(port), netstat.Listen)
				}
			}

		}
	}

}

func ServicePortScan(k8s bool) (*Set, *Set) {
	tcpSet := &Set{}
	udpSet := &Set{}

	hostPortScan(tcpSet, udpSet)
	containerPortScan(tcpSet, udpSet)
	if k8s {
		k8sPortScan(tcpSet, udpSet)
	}

	return tcpSet, udpSet
}
