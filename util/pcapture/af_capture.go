package pcapture

//package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/afpacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcap"
	"github.com/whatap/golib/logger/logfile"
	"golang.org/x/net/bpf"
)

type Pcap struct {
	Packet gopacket.Packet
	Time   time.Time
}
type AFPacketCapture struct {
	ifaces       map[string]*InterfaceInfo    // ifaceName:ipLIst
	tPackets     map[string]*afpacket.TPacket // ifaceName:tpacket
	done         map[string]chan bool
	PacketChans  chan *Pcap
	option       int
	logger       *logfile.FileLogger
	channelLimit int
}

func setAFPacketChannel(iface string) {

}

type InterfaceInfo struct {
	IP []string
}

func (afPacket *AFPacketCapture) FindNetworkInterfaceInfo() map[string]*InterfaceInfo {
	ifaces, err := pcap.FindAllDevs()
	if err != nil {
		//fmt.Println(err)
		return nil
	}

	ifaceMap := make(map[string]*InterfaceInfo)
	logmsg := ""

	for _, iface := range ifaces {
		info := &InterfaceInfo{}
		info.IP = make([]string, 0)
		for _, ip := range iface.Addresses {
			info.IP = append(info.IP, "dst host "+ip.IP.String())
		}

		if iface.Name == "any" || len(info.IP) == 0 { //|| strings.Contains(iface.Name, "veth") {
			continue
		}

		ifaceMap[iface.Name] = info
		if logmsg == "" {
			logmsg = fmt.Sprintf("%s(%s)", iface.Name, info.IP)
		} else {
			logmsg = fmt.Sprintf("%s, %s(%s)", logmsg, iface.Name, info.IP)
		}
	}

	afPacket.logger.Println("AF_PACKTE_FIND_INTERFACE", logmsg)
	afPacket.ifaces = ifaceMap

	return ifaceMap
}

func setBPFFilter(handle *afpacket.TPacket, filter string, snaplen int) (err error) {
	pcapBPF, err := pcap.CompileBPFFilter(layers.LinkTypeEthernet, snaplen, filter)
	if err != nil {
		return err
	}
	bpfIns := []bpf.RawInstruction{}
	for _, ins := range pcapBPF {
		bpfIns2 := bpf.RawInstruction{
			Op: ins.Code,
			Jt: ins.Jt,
			Jf: ins.Jf,
			K:  ins.K,
		}
		bpfIns = append(bpfIns, bpfIns2)
	}
	if handle.SetBPF(bpfIns); err != nil {
		return err
	}
	return nil
}

func (afPacket *AFPacketCapture) newTPackets(option int) {

	afPacket.tPackets = make(map[string]*afpacket.TPacket)
	afPacket.done = make(map[string]chan bool)
	for k, v := range afPacket.ifaces {
		tPacket, err := afpacket.NewTPacket(afpacket.OptInterface(k), afpacket.OptPollTimeout(1000000000)) //1 sec
		if err != nil {
			return
		}

		doneChannel := make(chan bool, 1)
		afPacket.tPackets[k] = tPacket
		afPacket.done[k] = doneChannel
		//afPacket.tPackets = append(afPacket.tPackets, tPacket)

		if option == 1 {
			filter := "tcp[tcpflags] & (tcp-syn|tcp-ack) == (tcp-syn|tcp-ack) and " + strings.Join(v.IP, " or ")
			setBPFFilter(tPacket, filter, 1024)
			afPacket.logger.Println(fmt.Sprintf("AF_PACKET_RUN(%s)", k), fmt.Sprintf("AF_PACKET Run(%s)", k))
			go afPacket.run(tPacket, doneChannel, k)
		}

	}
}

func (afPacket *AFPacketCapture) run(packet *afpacket.TPacket, done chan bool, k string) {
	source := gopacket.ZeroCopyPacketDataSource(packet)

	for {
		select {
		case <-done:
			packet.Close()
			close(done)
			afPacket.logger.Println("AFPacketClose", "AFPacket Close")
			return
		default:
		}

		data, ci, err := source.ZeroCopyReadPacketData()
		if err != nil {
			//fmt.Println("timeout", k)
			continue
		}
		time := time.Now()
		if !ci.Timestamp.IsZero() {
			time = ci.Timestamp
		}

		packet := gopacket.NewPacket(data, layers.LayerTypeEthernet, gopacket.Default)

		if len(afPacket.PacketChans) > afPacket.channelLimit {
			afPacket.logger.Println("AF_PACKET Run", "AF_PACKET Channel overflow")
			//
		}
		afPacket.PacketChans <- &Pcap{Packet: packet, Time: time}
	}
}

func (afpacket *AFPacketCapture) AllClose() {
	for k, _ := range afpacket.ifaces {
		afpacket.Close(k)
	}
	// close(afpacket.PacketChans)
}

func (afpacket *AFPacketCapture) Close(key string) {
	// Close
	afpacket.logger.Println("AF_PACKTE_CLOSE", fmt.Sprintf("AF_PACKET(interface: %s) Close", key))
	if _, ok := afpacket.ifaces[key]; ok {
		delete(afpacket.ifaces, key)
	}

	if _, ok := afpacket.tPackets[key]; ok {
		delete(afpacket.tPackets, key)
	}

	if done, ok := afpacket.done[key]; ok {
		done <- true
		delete(afpacket.done, key)
	}
}

func (afPacket *AFPacketCapture) Reload() {
	ifaces, err := pcap.FindAllDevs()
	if err != nil {
		fmt.Println(err)
		return
	}

	checkMap := make(map[string]bool)
	if afPacket.ifaces != nil {
		for k, _ := range afPacket.ifaces {
			checkMap[k] = true
		}
	}

	logmsg := ""
	for _, iface := range ifaces {
		if iface.Name == "any" { //|| strings.Contains(iface.Name, "veth") {
			continue
		}

		if _, ok := afPacket.ifaces[iface.Name]; ok {
			delete(checkMap, iface.Name)
		} else {
			delete(checkMap, iface.Name)
			info := &InterfaceInfo{}
			info.IP = make([]string, 0)
			for _, ip := range iface.Addresses {
				info.IP = append(info.IP, "dst host "+ip.IP.String())
			}
			if len(info.IP) == 0 {
				continue
			}
			afPacket.ifaces[iface.Name] = info

			tPacket, err := afpacket.NewTPacket(afpacket.OptInterface(iface.Name), afpacket.OptPollTimeout(1000000000)) //1 sec
			if err != nil {
				//fmt.Println(err)
				continue
			}

			doneChannel := make(chan bool, 1)
			afPacket.tPackets[iface.Name] = tPacket
			afPacket.done[iface.Name] = doneChannel
			//afPacket.tPackets = append(afPacket.tPackets, tPacket)

			if logmsg == "" {
				logmsg = fmt.Sprintf("%s(%s)", iface.Name, info.IP)
			} else {
				logmsg = fmt.Sprintf("%s, %s(%s)", logmsg, iface.Name, info.IP)
			}

			if afPacket.option == 1 {
				filter := "tcp[tcpflags] & (tcp-syn|tcp-ack) == (tcp-syn|tcp-ack) and " + strings.Join(info.IP, " or ")
				setBPFFilter(tPacket, filter, 1024)
				go afPacket.run(tPacket, doneChannel, iface.Name)
			}

		}
	}

	if logmsg != "" {
		afPacket.logger.Println("AF_PACKTE_FIND_NEW_INTERFACE", logmsg)
	}

	//Close
	for k, _ := range checkMap {
		afPacket.Close(k)
	}
}

func NewAFPacket(logger *logfile.FileLogger, channelSize, channelLimit int) *AFPacketCapture {
	afPacket := &AFPacketCapture{}
	afPacket.logger = logger
	logger.Println("AF_PACKET", fmt.Sprintf("AF Packet Capture Create (channel size : %d, channelLimit : %d) ", channelSize, channelLimit))
	afPacket.channelLimit = channelLimit
	afPacket.PacketChans = make(chan *Pcap, channelSize)

	afPacket.FindNetworkInterfaceInfo()
	afPacket.option = 1
	afPacket.newTPackets(1)

	return afPacket
}
