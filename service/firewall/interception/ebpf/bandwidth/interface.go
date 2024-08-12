package ebpf

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/rlimit"
	"golang.org/x/sys/unix"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/network/packet"
)

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -cflags "-O2 -g -Wall -Werror" bpf ../programs/bandwidth.c

var ebpfLoadingFailed atomic.Uint32

// BandwidthStatsWorker monitors connection bandwidth using ebpf.
func BandwidthStatsWorker(ctx context.Context, collectInterval time.Duration, bandwidthUpdates chan *packet.BandwidthUpdate) error {
	// Allow the current process to lock memory for eBPF resources.
	err := rlimit.RemoveMemlock()
	if err != nil {
		if ebpfLoadingFailed.Add(1) >= 5 {
			log.Warningf("ebpf: failed to remove memlock 5 times, giving up with error %s", err)
			return nil
		}
		return fmt.Errorf("ebpf: failed to remove memlock: %w", err)
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

	// Find the cgroup path
	path, err := findCgroupPath()
	if err != nil {
		return fmt.Errorf("ebpf: failed to find cgroup paths: %w", err)
	}

	// Attach socket options for monitoring connections
	sockOptionsLink, err := link.AttachCgroup(link.CgroupOptions{
		Path:    path,
		Program: objs.bpfPrograms.SocketOperations,
		Attach:  ebpf.AttachCGroupSockOps,
	})
	if err != nil {
		return fmt.Errorf("ebpf: failed to open module sockops: %w", err)
	}
	defer sockOptionsLink.Close() //nolint:errcheck

	// Attach Udp Ipv4 recive message tracing
	udpv4RMLink, err := link.AttachTracing(link.TracingOptions{
		Program: objs.bpfPrograms.UdpRecvmsg,
	})
	if err != nil {
		return fmt.Errorf("ebpf: failed to open trace Udp IPv4 recvmsg: %w", err)
	}
	defer udpv4RMLink.Close() //nolint:errcheck

	// Attach UDP IPv4 send message tracing
	udpv4SMLink, err := link.AttachTracing(link.TracingOptions{
		Program: objs.bpfPrograms.UdpSendmsg,
	})
	if err != nil {
		return fmt.Errorf("ebpf: failed to open trace Udp IPv4 sendmsg: %w", err)
	}
	defer udpv4SMLink.Close() //nolint:errcheck

	// Attach UDP IPv6 receive message tracing
	udpv6RMLink, err := link.AttachTracing(link.TracingOptions{
		Program: objs.bpfPrograms.Udpv6Recvmsg,
	})
	if err != nil {
		return fmt.Errorf("ebpf: failed to open trace Udp IPv6 recvmsg: %w", err)
	}
	defer udpv6RMLink.Close() //nolint:errcheck

	// Attach UDP IPv6 send message tracing
	udpv6SMLink, err := link.AttachTracing(link.TracingOptions{
		Program: objs.bpfPrograms.Udpv6Sendmsg,
	})
	if err != nil {
		return fmt.Errorf("ebpf: failed to open trace Udp IPv6 sendmsg: %w", err)
	}
	defer udpv6SMLink.Close() //nolint:errcheck

	// Setup ticker.
	ticker := time.NewTicker(collectInterval)
	defer ticker.Stop()

	// Collect bandwidth at every tick.
	for {
		select {
		case <-ticker.C:
			reportBandwidth(ctx, objs, bandwidthUpdates)
		case <-ctx.Done():
			return nil
		}
	}
}

// reportBandwidth reports the bandwidth to the given updates channel.
func reportBandwidth(ctx context.Context, objs bpfObjects, bandwidthUpdates chan *packet.BandwidthUpdate) {
	var (
		skKey   bpfSkKey
		skInfo  bpfSkInfo
		updated int
		skipped int
	)

	iter := objs.bpfMaps.PmBandwidthMap.Iterate()
	for iter.Next(&skKey, &skInfo) {
		// Check if already reported.
		if skInfo.Reported >= 1 {
			skipped++
			continue
		}
		// Mark as reported and update the map.
		skInfo.Reported = 1
		if err := objs.bpfMaps.PmBandwidthMap.Update(&skKey, &skInfo, ebpf.UpdateExist); err != nil {
			log.Debugf("ebpf: failed to mark bandwidth map entry as reported: %s", err)
		}

		connID := packet.CreateConnectionID(
			packet.IPProtocol(skKey.Protocol),
			convertArrayToIP(skKey.SrcIp, skKey.Ipv6 == 1), skKey.SrcPort,
			convertArrayToIP(skKey.DstIp, skKey.Ipv6 == 1), skKey.DstPort,
			false,
		)
		update := &packet.BandwidthUpdate{
			ConnID:        connID,
			BytesReceived: skInfo.Rx,
			BytesSent:     skInfo.Tx,
			Method:        packet.Absolute,
		}
		select {
		case bandwidthUpdates <- update:
			updated++
		case <-ctx.Done():
			return
		default:
			log.Warningf("ebpf: bandwidth update queue is full (updated=%d, skipped=%d), ignoring rest of batch", updated, skipped)
			return
		}
	}
}

// findCgroupPath returns the default unified path of the cgroup.
func findCgroupPath() (string, error) {
	cgroupPath := "/sys/fs/cgroup"

	var st syscall.Statfs_t
	err := syscall.Statfs(cgroupPath, &st)
	if err != nil {
		return "", err
	}
	isCgroupV2Enabled := st.Type == unix.CGROUP2_SUPER_MAGIC
	if !isCgroupV2Enabled {
		cgroupPath = filepath.Join(cgroupPath, "unified")
	}
	return cgroupPath, nil
}

// convertArrayToIP converts an array of uint32 values to a net.IP address.
func convertArrayToIP(input [4]uint32, ipv6 bool) net.IP {
	if !ipv6 {
		addressBuf := make([]byte, 4)
		binary.LittleEndian.PutUint32(addressBuf, input[0])
		return net.IP(addressBuf)
	} else {
		addressBuf := make([]byte, 16)
		for i := range 4 {
			binary.LittleEndian.PutUint32(addressBuf[i*4:i*4+4], input[i])
		}
		return net.IP(addressBuf)
	}
}
