package ebpf

import (
	"bytes"
	"encoding/binary"
	"errors"
	"net"

	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/network/packet"
)

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -cflags "-O2 -g -Wall -Werror" -type Event bpf ../programs/monitor.c

var stopper chan struct{}

// StartEBPFWorker starts the ebpf worker.
func StartEBPFWorker(ch chan packet.Packet) {
	stopper = make(chan struct{})
	go func() {
		// Allow the current process to lock memory for eBPF resources.
		if err := rlimit.RemoveMemlock(); err != nil {
			log.Errorf("ebpf: failed to remove ebpf memlock: %s", err)
			return
		}

		// Load pre-compiled programs and maps into the kernel.
		objs := bpfObjects{}
		if err := loadBpfObjects(&objs, nil); err != nil {
			log.Errorf("ebpf: failed to load ebpf object: %s", err)
			return
		}
		defer objs.Close() //nolint:errcheck

		// Create a link to the tcp_connect program.
		linkTCPConnect, err := link.AttachTracing(link.TracingOptions{
			Program: objs.bpfPrograms.TcpConnect,
		})
		if err != nil {
			log.Errorf("ebpf: failed to attach to tcp_v4_connect: %s ", err)
			return
		}
		defer linkTCPConnect.Close() //nolint:errcheck

		// Create a link to the udp_v4_connect program.
		linkUDPV4, err := link.AttachTracing(link.TracingOptions{
			Program: objs.bpfPrograms.UdpV4Connect,
		})
		if err != nil {
			log.Errorf("ebpf: failed to attach to udp_v4_connect: %s ", err)
			return
		}
		defer linkUDPV4.Close() //nolint:errcheck

		// Create a link to the udp_v6_connect program.
		linkUDPV6, err := link.AttachTracing(link.TracingOptions{
			Program: objs.bpfPrograms.UdpV6Connect,
		})
		if err != nil {
			log.Errorf("ebpf: failed to attach to udp_v6_connect: %s ", err)
			return
		}
		defer linkUDPV6.Close() //nolint:errcheck

		rd, err := ringbuf.NewReader(objs.bpfMaps.PmConnectionEvents)
		if err != nil {
			log.Errorf("ebpf: failed to open ring buffer: %s", err)
			return
		}
		defer rd.Close()

		go func() {
			<-stopper

			if err := rd.Close(); err != nil {
				log.Errorf("ebpf: failed closing ringbuf reader: %s", err)
			}
		}()

		for {
			// Read next event
			record, err := rd.Read()
			if err != nil {
				if errors.Is(err, ringbuf.ErrClosed) {
					// Normal return
					return
				}
				log.Errorf("ebpf: failed to read from ring buffer: %s", err)
				continue
			}

			var event bpfEvent
			// Parse the ringbuf event entry into a bpfEvent structure.
			if err := binary.Read(bytes.NewBuffer(record.RawSample), binary.BigEndian, &event); err != nil {
				log.Errorf("ebpf: failed to parse ringbuf event: %s", err)
				continue
			}

			info := packet.Info{
				Inbound:  event.Direction == 1,
				InTunnel: false,
				Version:  packet.IPVersion(event.IpVersion),
				Protocol: packet.IPProtocol(event.Protocol),
				SrcPort:  event.Sport,
				DstPort:  event.Dport,
				Src:      convertArrayToIPv4(event.Saddr, packet.IPVersion(event.IpVersion)),
				Dst:      convertArrayToIPv4(event.Daddr, packet.IPVersion(event.IpVersion)),
				PID:      int(event.Pid),
			}
			if isEventValid(event) {
				log.Debugf("ebpf: PID: %d conn: %s:%d -> %s:%d %s %s", info.PID, info.LocalIP(), info.LocalPort(), info.RemoteIP(), info.RemotePort(), info.Version.String(), info.Protocol.String())

				p := &infoPacket{}
				p.SetPacketInfo(info)
				ch <- p
			} else {
				log.Debugf("ebpf: invalid event PID: %d conn: %s:%d -> %s:%d %s %s", info.PID, info.LocalIP(), info.LocalPort(), info.RemoteIP(), info.RemotePort(), info.Version.String(), info.Protocol.String())
			}

		}
	}()
}

// StopEBPFWorker stops the ebpf worker.
func StopEBPFWorker() {
	close(stopper)
}

// isEventValid checks whether the given bpfEvent is valid or not.
// It returns true if the event is valid, otherwise false.
func isEventValid(event bpfEvent) bool {
	// Check if the destination port is 0
	if event.Dport == 0 {
		return false
	}

	// Check if the source port is 0
	if event.Sport == 0 {
		return false
	}

	// Check if the process ID is 0
	if event.Pid == 0 {
		return false
	}

	// If the IP version is IPv4
	if event.IpVersion == 4 {
		if event.Saddr[0] == 0 {
			return false
		}

		if event.Daddr[0] == 0 {
			return false
		}
	}
	return true
}

// convertArrayToIPv4 converts an array of uint32 values to an IPv4 net.IP address.
func convertArrayToIPv4(input [4]uint32, ipVersion packet.IPVersion) net.IP {
	if ipVersion == packet.IPv4 {
		addressBuf := make([]byte, 4)
		binary.LittleEndian.PutUint32(addressBuf, input[0])
		return net.IP(addressBuf)
	} else {
		addressBuf := make([]byte, 16)
		for i := 0; i < 4; i++ {
			binary.LittleEndian.PutUint32(addressBuf[i*4:i*4+4], input[i])
		}
		return net.IP(addressBuf)
	}
}