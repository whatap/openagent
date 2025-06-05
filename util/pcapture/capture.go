package pcapture

import (
	"log"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/pcap"
)

// //////////////////////////////////////////////////////////////////////////////////////////////
// capture util
type PacketCapture struct {
	ifaces []string
	//	handle     *pacp.Handle
	PacketChans []chan gopacket.Packet
}

func NewPacketCapture() *PacketCapture {
	capture := &PacketCapture{}
	capture.ifaces = findNetworkDriver()
	capture.PacketChans = make([]chan gopacket.Packet, 0)

	for _, iface := range capture.ifaces {
		capture.setPacketCaptureChannel(iface)
	}
	return capture
}

func (capture *PacketCapture) setPacketCaptureChannel(iface string) {

	deviceName := iface
	//"\\Device\\NPF_{8D5E7EF6-59FC-4D1E-BC81-A93361AE42C4}" // 사용할 네트워크 인터페이스의 이름을 설정
	snapshotLen := int32(1024)
	promiscuous := false
	timeout := 100 * time.Millisecond

	handle, err := pcap.OpenLive(deviceName, snapshotLen, promiscuous, timeout)

	if err != nil {
		log.Println(err)
		return
	}

	handle.SetBPFFilter("tcp or udp")

	packetSource := gopacket.NewPacketSource(handle, handle.LinkType())

	capture.PacketChans = append(capture.PacketChans, packetSource.Packets())

}
func findNetworkDriver() []string {
	// 네트워크 인터페이스 목록 조회
	ifaces, err := pcap.FindAllDevs()
	if err != nil {
		log.Fatal(err)
	}

	ifaceList := make([]string, 0)
	// 각 인터페이스의 정보 출력
	for _, iface := range ifaces {
		ifaceList = append(ifaceList, iface.Name)
	}

	return ifaceList
}

func AnyPacketCaptureChannel() chan gopacket.Packet {
	deviceName := "any"
	snapshotLen := int32(1024)
	promiscuous := false
	timeout := 100 * time.Millisecond

	handle, err := pcap.OpenLive(deviceName, snapshotLen, promiscuous, timeout)

	if err != nil {
		log.Println(err)
		return nil
	}

	handle.SetBPFFilter("tcp or udp")

	packetSource := gopacket.NewPacketSource(handle, handle.LinkType())

	return packetSource.Packets()
}
