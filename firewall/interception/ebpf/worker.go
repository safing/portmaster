package ebpf

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"unsafe"

	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/network/packet"
)

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -cflags "-O2 -g -Wall -Werror" -type event bpf program/monitor.c
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
			ipVersion := packet.IPVersion(event.IpVersion)
			log.Debugf("ebpf: pid:%d connection %s -> %s ", int(event.Pid), intToIP(event.Saddr, ipVersion).String()+":"+fmt.Sprintf("%d", event.Sport), intToIP(event.Daddr, ipVersion).String()+":"+fmt.Sprintf("%d", event.Dport))
			pack := &infoPacket{}

			pack.SetPacketInfo(packet.Info{
				Inbound:  false,
				InTunnel: false,
				Version:  packet.IPVersion(event.IpVersion),
				Protocol: packet.TCP,
				SrcPort:  event.Sport,
				DstPort:  event.Dport,
				Src:      intToIP(event.Saddr, ipVersion),
				Dst:      intToIP(event.Daddr, ipVersion),
				PID:      event.Pid,
			})
			ch <- pack
		}
	}()
}

func StopEBPFWorker() {
	close(stopper)
}

// intToIP converts IPv4 number to net.IP
func intToIP(ipNum [4]uint32, ipVersion packet.IPVersion) net.IP {
	if ipVersion == packet.IPv4 {
		return unsafe.Slice((*byte)(unsafe.Pointer(&ipNum)), 4)
	} else {
		return unsafe.Slice((*byte)(unsafe.Pointer(&ipNum)), 16)
	}
}
