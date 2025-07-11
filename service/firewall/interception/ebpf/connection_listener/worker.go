package ebpf

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync/atomic"
	"time"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/btf"
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
	if err := loadBpfObjects_Ex(&objs, nil); err != nil {
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

// loadBpfObjects_Ex loads eBPF objects with kernel-aware attach point selection.
// (it extends the standard loadBpfObjects())
//
// This enhanced loader automatically detects available kernel functions and selects
// appropriate attach points for maximum compatibility across kernel versions. It handles
// the transition from legacy function names (e.g., ip4_datagram_connect) to modern ones
// (e.g., udp_connect) introduced in Linux 6.13+.
func loadBpfObjects_Ex(objs interface{}, opts *ebpf.CollectionOptions) error {
	// Load pre-compiled programs
	spec, err := loadBpf()
	if err != nil {
		return fmt.Errorf("ebpf: failed to load ebpf spec: %w", err)
	}
	// Modify the attach points of the eBPF programs, if necessary.
	if err := modifyProgramsAttachPoints(spec); err != nil {
		return fmt.Errorf("ebpf: failed to modify program attach points: %w", err)
	}
	// Load the eBPF programs and maps into the kernel.
	if err := spec.LoadAndAssign(objs, opts); err != nil {
		return fmt.Errorf("ebpf: failed to load and assign ebpf objects: %w", err)
	}
	return nil
}

// modifyProgramAttachPoints modifies the attach points of the eBPF programs, if necessary.
// This is needed to ensure compatibility with different kernel versions.
func modifyProgramsAttachPoints(spec *ebpf.CollectionSpec) error {
	// Load the kernel spec
	kspec, err := btf.LoadKernelSpec()
	if err != nil {
		return err
	}

	// Function to update the attach point to a single BPF program
	updateIfNeeded := func(bpfProgramName, attachPoint, attachPointLegacy string) error {
		ps, ok := spec.Programs[bpfProgramName]
		if !ok {
			return fmt.Errorf("ebpf: program %q not found in spec", bpfProgramName)
		}
		var fn *btf.Func
		if err := kspec.TypeByName(attachPoint, &fn); err == nil {
			if !strings.EqualFold(ps.AttachTo, attachPoint) {
				ps.AttachTo = attachPoint
				log.Debugf("ebpf: using attach point %q for %q program", attachPoint, bpfProgramName)
			}
		} else {
			if !strings.EqualFold(ps.AttachTo, attachPointLegacy) {
				ps.AttachTo = attachPointLegacy
				log.Debugf("ebpf: using legacy attach point %q for %q program", attachPointLegacy, bpfProgramName)
			}
		}
		return nil
	}

	// 'udp_v4_connect' program is designed to attach to the `udp_connect` function in the kernel.
	// If the kernel does not support this function, we fall back to using the `ip4_datagram_connect` function.
	//
	// Kernel compatibility note:
	// - Linux kernels < 6.13: use `ip4_datagram_connect` function
	//   https://elixir.bootlin.com/linux/v6.12.34/source/net/ipv4/udp.c#L2997
	// - Linux kernels >= 6.13: function renamed to `udp_connect`
	//   https://elixir.bootlin.com/linux/v6.13-rc1/source/net/ipv4/udp.c#L3131
	const (
		udpV4ConnectProgramName       = "udp_v4_connect"
		udpV4ConnectAttachPoint       = "udp_connect"
		udpV4ConnectAttachPointLegacy = "ip4_datagram_connect"
	)
	if err := updateIfNeeded(udpV4ConnectProgramName, udpV4ConnectAttachPoint, udpV4ConnectAttachPointLegacy); err != nil {
		return err
	}

	// 'udp_v6_connect' program is designed to attach to the `udpv6_connect` function in the kernel.
	// If the kernel does not support this function, we fall back to using the `ip6_datagram_connect` function.
	//
	// Kernel compatibility note:
	// - Linux kernels < 6.13: use `ip6_datagram_connect` function
	//   https://elixir.bootlin.com/linux/v6.12.34/source/net/ipv4/udp.c#L2997
	// - Linux kernels >= 6.13: function renamed to `udpv6_connect`
	//   https://elixir.bootlin.com/linux/v6.13-rc1/source/net/ipv4/udp.c#L3131
	const (
		udpV6ConnectProgramName       = "udp_v6_connect"
		udpV6ConnectAttachPoint       = "udpv6_connect"
		udpV6ConnectAttachPointLegacy = "ip6_datagram_connect"
	)
	if err := updateIfNeeded(udpV6ConnectProgramName, udpV6ConnectAttachPoint, udpV6ConnectAttachPointLegacy); err != nil {
		return err
	}

	return nil
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
