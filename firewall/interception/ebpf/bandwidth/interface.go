package ebpf

import (
	"encoding/binary"
	"fmt"
	"net"
	"path/filepath"
	"syscall"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/rlimit"
	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/network/packet"
	"golang.org/x/sys/unix"
)

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -cflags "-O2 -g -Wall -Werror" bpf ../programs/bandwidth.c

var ebpfInterface = struct {
	objs            bpfObjects
	sockOptionsLink link.Link
	udpv4SMLink     link.Link
	udpv4RMLink     link.Link
	udpv6SMLink     link.Link
	udpv6RMLink     link.Link
}{
	objs: bpfObjects{},
}

// SetupBandwidthInterface initializes the ebpf interface and starts gattering bandwidth information for all connections.
func SetupBandwidthInterface() error {

	// Allow the current process to lock memory for eBPF resources.
	err := rlimit.RemoveMemlock()
	if err != nil {
		return fmt.Errorf("failed to remove memlock: %s", err)
	}

	// Load pre-compiled programs and maps into the kernel.
	err = loadBpfObjects(&ebpfInterface.objs, nil)
	if err != nil {
		return fmt.Errorf("feiled loading objects: %s", err)
	}

	defer func() {
		if err != nil {
			// Defer the cleanup function to be called at the end of the enclosing function
			// If there was an error during the execution, shutdown the BandwithInterface
			ShutdownBandwithInterface()
		}
	}()

	// Find the cgroup path
	path, err := findCgroupPath()
	if err != nil {
		return fmt.Errorf("faield to find cgroup paths: %s", err)
	}

	// Attach socket options for monitoring connections
	ebpfInterface.sockOptionsLink, err = link.AttachCgroup(link.CgroupOptions{
		Path:    path,
		Program: ebpfInterface.objs.bpfPrograms.SocketOperations,
		Attach:  ebpf.AttachCGroupSockOps,
	})
	if err != nil {
		return fmt.Errorf("Failed to open module sockops: %s", err)
	}

	// Attach Udp Ipv4 recive message tracing
	ebpfInterface.udpv4RMLink, err = link.AttachTracing(link.TracingOptions{
		Program: ebpfInterface.objs.UdpRecvmsg,
	})
	if err != nil {
		return fmt.Errorf("Failed to open trace Udp IPv4 recvmsg: %s", err)
	}

	// Attach UDP IPv4 send message tracing
	ebpfInterface.udpv4SMLink, err = link.AttachTracing(link.TracingOptions{
		Program: ebpfInterface.objs.UdpSendmsg,
	})
	if err != nil {
		return fmt.Errorf("Failed to open trace Udp IPv4 sendmsg: %s", err)
	}

	// Attach UDP IPv6 receive message tracing
	ebpfInterface.udpv6RMLink, err = link.AttachTracing(link.TracingOptions{
		Program: ebpfInterface.objs.Udpv6Recvmsg,
	})
	if err != nil {
		return fmt.Errorf("Failed to open trace Udp IPv6 recvmsg: %s", err)
	}

	// Attach UDP IPv6 send message tracing
	ebpfInterface.udpv6RMLink, err = link.AttachTracing(link.TracingOptions{
		Program: ebpfInterface.objs.Udpv6Sendmsg,
	})
	if err != nil {
		return fmt.Errorf("Failed to open trace Udp IPv6 sendmsg: %s", err)
	}

	// Example code that will print the bandwidth table every 10 seconds
	// go func() {
	// 	ticker := time.NewTicker(10 * time.Second)
	// 	defer ticker.Stop()
	// 	for range ticker.C {
	// 		printBandwidthData()
	// 	}
	// }()

	return nil
}

// ShutdownBandwithInterface shuts down the bandwidth interface by closing the associated links and objects.
func ShutdownBandwithInterface() {
	// Close the sockOptionsLink if it is not nil
	if ebpfInterface.sockOptionsLink != nil {
		ebpfInterface.sockOptionsLink.Close()
	}

	// Close the udpv4SMLink if it is not nil
	if ebpfInterface.udpv4SMLink != nil {
		ebpfInterface.udpv4SMLink.Close()
	}

	// Close the udpv4RMLink if it is not nil
	if ebpfInterface.udpv4RMLink != nil {
		ebpfInterface.udpv4RMLink.Close()
	}

	// Close the udpv6SMLink if it is not nil
	if ebpfInterface.udpv6SMLink != nil {
		ebpfInterface.udpv6SMLink.Close()
	}

	// Close the udpv6RMLink if it is not nil
	if ebpfInterface.udpv6RMLink != nil {
		ebpfInterface.udpv6RMLink.Close()
	}

	// Close the ebpfInterface objects
	ebpfInterface.objs.Close()
}

// findCgroupPath returns the default unified path of the cgroup
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

// printBandwidthData prints the contencs of the shared map in the ebpf program.
func printBandwidthData() {
	iter := ebpfInterface.objs.bpfMaps.PmBandwidthMap.Iterate()
	var skKey bpfSkKey
	var skInfo bpfSkInfo
	for iter.Next(&skKey, &skInfo) {
		log.Debugf("Connection: %d %s:%d %s:%d %d %d", skKey.Protocol,
			convertArrayToIPv4(skKey.SrcIp, packet.IPVersion(skKey.Ipv6)).String(), skKey.SrcPort,
			convertArrayToIPv4(skKey.DstIp, packet.IPVersion(skKey.Ipv6)).String(), skKey.DstPort,
			skInfo.Rx, skInfo.Tx,
		)
	}
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