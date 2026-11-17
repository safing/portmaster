//go:build linux

package ebpf

import (
	"context"
	"encoding/binary"
	"errors"
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
	reporter := newBandwidthReporter()
	source := &bandwidthMapSource{bpfMap: objs.PmBandwidthMap}
	for {
		select {
		case <-ticker.C:
			reporter.report(ctx, source, bandwidthUpdates)
		case <-ctx.Done():
			return nil
		}
	}
}

// bandwidthSource provides access to the absolute byte counters that the
// eBPF programs keep per connection.
type bandwidthSource interface {
	// forEach calls fn for every entry until fn returns false.
	// The returned error tells that the iteration did not see all entries.
	forEach(fn func(key bpfSkKey, info bpfSkInfo) (proceed bool)) error
	// lookup returns the entry with the given key, if it exists.
	lookup(key bpfSkKey) (info bpfSkInfo, exists bool, err error)
}

// bandwidthMapSource is the bandwidthSource of the eBPF bandwidth map.
type bandwidthMapSource struct {
	bpfMap *ebpf.Map
}

func (s *bandwidthMapSource) forEach(fn func(key bpfSkKey, info bpfSkInfo) (proceed bool)) error {
	var (
		skKey  bpfSkKey
		skInfo bpfSkInfo
	)
	iter := s.bpfMap.Iterate()
	for iter.Next(&skKey, &skInfo) {
		if !fn(skKey, skInfo) {
			return nil
		}
	}
	return iter.Err()
}

func (s *bandwidthMapSource) lookup(key bpfSkKey) (info bpfSkInfo, exists bool, err error) {
	err = s.bpfMap.Lookup(&key, &info)
	switch {
	case err == nil:
		return info, true, nil
	case errors.Is(err, ebpf.ErrKeyNotExist):
		return info, false, nil
	default:
		return info, false, err
	}
}

// reportedBandwidth holds the counter values of a connection that were
// reported already.
type reportedBandwidth struct {
	rx uint64
	tx uint64
	// seenInRound is the last report round the connection was seen in.
	seenInRound uint64
}

// bandwidthReporter turns the absolute byte counters of the eBPF bandwidth
// map into additive bandwidth updates.
//
// The counters in the map are kept per connection 5-tuple and outlive the
// connections of Portmaster: The entry of a UDP socket keeps counting when a
// connection ended and a new one with the same 5-tuple was started. Reporting
// the counters as absolute values attributed all previous bytes to the new
// connection. Instead, remember what was reported for every entry and only
// report what was added since.
//
// The reporter only reads from the map. It previously marked reported entries
// in the map, but writing the whole entry back replaced the counters with the
// values read before, which lost the bytes that were counted in between.
type bandwidthReporter struct {
	reported map[bpfSkKey]*reportedBandwidth
	round    uint64
}

func newBandwidthReporter() *bandwidthReporter {
	return &bandwidthReporter{
		reported: make(map[bpfSkKey]*reportedBandwidth),
	}
}

// report reports the bandwidth added since the last report to the given
// updates channel.
func (r *bandwidthReporter) report(ctx context.Context, source bandwidthSource, bandwidthUpdates chan *packet.BandwidthUpdate) {
	var (
		updated int
		skipped int
		aborted bool
	)
	r.round++

	// reportEntry reports a single entry and returns whether to continue.
	reportEntry := func(skKey bpfSkKey, skInfo bpfSkInfo) (proceed bool) {
		previous := r.reported[skKey]

		// Calculate what was added since the last report.
		var rx, tx uint64
		if previous != nil {
			previous.seenInRound = r.round
			rx = bandwidthCounterDelta(previous.rx, skInfo.Rx)
			tx = bandwidthCounterDelta(previous.tx, skInfo.Tx)
		} else {
			rx = skInfo.Rx
			tx = skInfo.Tx
		}
		if rx == 0 && tx == 0 {
			skipped++
			return true
		}

		connID := packet.CreateConnectionID(
			packet.IPProtocol(skKey.Protocol),
			convertArrayToIP(skKey.SrcIp, skKey.Ipv6 == 1), skKey.SrcPort,
			convertArrayToIP(skKey.DstIp, skKey.Ipv6 == 1), skKey.DstPort,
			false,
		)
		update := &packet.BandwidthUpdate{
			ConnID:        connID,
			BytesReceived: rx,
			BytesSent:     tx,
			Method:        packet.Additive,
		}
		select {
		case bandwidthUpdates <- update:
			updated++
		case <-ctx.Done():
			aborted = true
			return false
		default:
			// The reported values are not updated for the remaining entries,
			// so their bytes are included in the next report.
			log.Warningf("ebpf: bandwidth update queue is full (updated=%d, skipped=%d), ignoring rest of batch", updated, skipped)
			aborted = true
			return false
		}

		// Remember what was reported, now that the update is on its way.
		if previous == nil {
			previous = &reportedBandwidth{seenInRound: r.round}
			r.reported[skKey] = previous
		}
		previous.rx = skInfo.Rx
		previous.tx = skInfo.Tx
		return true
	}

	err := source.forEach(reportEntry)
	if err != nil {
		log.Debugf("ebpf: failed to iterate the bandwidth map: %s", err)
		return
	}
	if aborted {
		return
	}

	// Forget entries that are gone from the map.
	// The iteration may miss entries while the map is changed, so look up
	// every entry that was not seen: Forgetting an entry that still exists
	// would report all of its bytes again.
	for skKey, previous := range r.reported {
		if previous.seenInRound == r.round {
			continue
		}
		skInfo, exists, err := source.lookup(skKey)
		switch {
		case err != nil:
			// Keep the entry and check again next time.
		case !exists:
			delete(r.reported, skKey)
		case !reportEntry(skKey, skInfo):
			return
		}
	}
}

// bandwidthCounterDelta returns what was added to an absolute byte counter
// between the previous and the current value. A counter that went backwards
// was reset, eg. because the entry was replaced for a new socket with the same
// 5-tuple. It then holds what was counted since the reset.
func bandwidthCounterDelta(previous, current uint64) uint64 {
	if current < previous {
		return current
	}
	return current - previous
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
