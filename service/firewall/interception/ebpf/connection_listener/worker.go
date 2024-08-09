package ebpf

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"sync/atomic"
	"time"

	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/network/packet"
)

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -cflags "-O2 -g -Wall -Werror" -type Event bpf ../programs/monitor.c

var ebpfLoadingFailed atomic.Uint32

// ConnectionListenerWorker listens to new connections using ebpf.
func ConnectionListenerWorker(ctx context.Context, packets chan packet.Packet) error {
	// Allow the current process to lock memory for eBPF resources.
	if err := rlimit.RemoveMemlock(); err != nil {
		if ebpfLoadingFailed.Add(1) >= 5 {
			log.Warningf("ebpf: failed to remove memlock 5 times, giving up with error %s", err)
			return nil
		}
		return fmt.Errorf("ebpf: failed to remove ebpf memlock: %w", err)
	}

	// Load pre-compiled programs and maps into the kernel.
	objs := bpfObjects{}
	if err := loadBpfObjects(&objs, nil); err != nil {
		if ebpfLoadingFailed.Add(1) >= 5 {
			log.Warningf("ebpf: failed to load ebpf object 5 times, giving up with error %s", err)
			return nil
		}
		return fmt.Errorf("ebpf: failed to load ebpf object: %w", err)
	}
	defer objs.Close() //nolint:errcheck

	// Create a link to the tcp_connect program.
	linkTCPConnect, err := link.AttachTracing(link.TracingOptions{
		Program: objs.bpfPrograms.TcpConnect,
	})
	if err != nil {
		return fmt.Errorf("ebpf: failed to attach to tcp_v4_connect: %w", err)
	}
	defer linkTCPConnect.Close() //nolint:errcheck

	// Create a link to the udp_v4_connect program.
	linkUDPV4, err := link.AttachTracing(link.TracingOptions{
		Program: objs.bpfPrograms.UdpV4Connect,
	})
	if err != nil {
		return fmt.Errorf("ebpf: failed to attach to udp_v4_connect: %w", err)
	}
	defer linkUDPV4.Close() //nolint:errcheck

	// Create a link to the udp_v6_connect program.
	linkUDPV6, err := link.AttachTracing(link.TracingOptions{
		Program: objs.bpfPrograms.UdpV6Connect,
	})
	if err != nil {
		return fmt.Errorf("ebpf: failed to attach to udp_v6_connect: %w", err)
	}
	defer linkUDPV6.Close() //nolint:errcheck

	// Create new reader to read events.
	rd, err := ringbuf.NewReader(objs.bpfMaps.PmConnectionEvents)
	if err != nil {
		return fmt.Errorf("ebpf: failed to open ring buffer: %w", err)
	}
	defer rd.Close() //nolint:errcheck

	// Start watcher to close the reader when the context is canceled.
	// TODO: Can we put this into a worker?
	go func() {
		<-ctx.Done()

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
				return nil
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

		pkt := packet.NewInfoPacket(packet.Info{
			Inbound:  event.Direction == 1,
			InTunnel: false,
			Version:  packet.IPVersion(event.IpVersion),
			Protocol: packet.IPProtocol(event.Protocol),
			SrcPort:  event.Sport,
			DstPort:  event.Dport,
			Src:      convertArrayToIPv4(event.Saddr, packet.IPVersion(event.IpVersion)),
			Dst:      convertArrayToIPv4(event.Daddr, packet.IPVersion(event.IpVersion)),
			PID:      int(event.Pid),
			SeenAt:   time.Now(),
		})
		if isEventValid(event) {
			// DEBUG:
			// log.Debugf("ebpf: received valid connect event: PID: %d Conn: %s", pkt.Info().PID, pkt)
			packets <- pkt
		} else {
			log.Warningf("ebpf: received invalid connect event: PID: %d Conn: %s", pkt.Info().PID, pkt)
		}
	}
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
	}

	addressBuf := make([]byte, 16)
	for i := range 4 {
		binary.LittleEndian.PutUint32(addressBuf[i*4:i*4+4], input[i])
	}
	return net.IP(addressBuf)
}
