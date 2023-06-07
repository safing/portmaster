package ebpf

import (
	"bytes"
	"encoding/binary"
	"errors"
	"net"
	"unsafe"

	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/network/packet"
)

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -cflags "-O2 -g -Wall -Werror" -type Event bpf program/monitor.c
var stopper chan struct{}

func StartEBPFWorker(ch chan packet.Packet) {
	stopper = make(chan struct{})
	go func() {
		// Allow the current process to lock memory for eBPF resources.
		if err := rlimit.RemoveMemlock(); err != nil {
			log.Errorf("ebpf: failed to remove ebpf memlock: %s", err)
		}

		// Load pre-compiled programs and maps into the kernel.
		objs := bpfObjects{}
		if err := loadBpfObjects(&objs, nil); err != nil {
			log.Errorf("ebpf: failed to load ebpf object: %s", err)
		}
		defer objs.Close()

		// Create a link to the tcp_v4_connect program.
		linkv4, err := link.AttachTracing(link.TracingOptions{
			Program: objs.bpfPrograms.TcpV4Connect,
		})
		if err != nil {
			log.Errorf("ebpf: failed to attach to tcp_v4_connect: %s ", err)
		}
		defer linkv4.Close()

		// Create a link to the tcp_v6_connect program.
		linkv6, err := link.AttachTracing(link.TracingOptions{
			Program: objs.bpfPrograms.TcpV6Connect,
		})
		if err != nil {
			log.Errorf("ebpf: failed to attach to tcp_v6_connect: %s ", err)
		}
		defer linkv6.Close()

		// Create a link to the tcp_v6_connect program.
		linkudp, err := link.AttachTracing(link.TracingOptions{
			Program: objs.bpfPrograms.UdpSendmsg,
		})
		if err != nil {
			log.Errorf("ebpf: failed to attach to udp_sendmsg: %s ", err)
		}
		defer linkudp.Close()

		rd, err := ringbuf.NewReader(objs.bpfMaps.Events)
		if err != nil {
			log.Errorf("ebpf: failed to open ring buffer: %s", err)
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
				Inbound:  false,
				InTunnel: false,
				Version:  packet.IPVersion(event.IpVersion),
				Protocol: packet.IPProtocol(event.Protocol),
				SrcPort:  event.Sport,
				DstPort:  event.Dport,
				Src:      arrayToIP(event.Saddr, packet.IPVersion(event.IpVersion)),
				Dst:      arrayToIP(event.Daddr, packet.IPVersion(event.IpVersion)),
				PID:      event.Pid,
			}
			log.Debugf("ebpf: PID: %d conn: %s:%d -> %s:%d %s %s", info.PID, info.LocalIP(), info.LocalPort(), info.RemoteIP(), info.LocalPort(), info.Version.String(), info.Protocol.String())

			p := &infoPacket{}
			p.SetPacketInfo(info)
			ch <- p
		}
	}()
}

func StopEBPFWorker() {
	close(stopper)
}

// arrayToIP converts IPv4 number to net.IP
func arrayToIP(ipNum [4]uint32, ipVersion packet.IPVersion) net.IP {
	if ipVersion == packet.IPv4 {
		return unsafe.Slice((*byte)(unsafe.Pointer(&ipNum)), 4)
	} else {
		return unsafe.Slice((*byte)(unsafe.Pointer(&ipNum)), 16)
	}
}
