//go:build windows

package iphelper

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"sync"
	"unsafe"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/network/socket"

	"golang.org/x/sys/windows"
)

// Windows API constants
const (
	iphelperTCPTableOwnerPIDAll uintptr = 5
	iphelperUDPTableOwnerPID    uintptr = 1
	iphelperTCPStateListen      uint32  = 2

	winErrInsufficientBuffer = uintptr(windows.ERROR_INSUFFICIENT_BUFFER)
	winErrInvalidParameter   = uintptr(windows.ERROR_INVALID_PARAMETER)
)

type iphelperTCPTable struct {
	// docs: https://msdn.microsoft.com/en-us/library/windows/desktop/aa366921(v=vs.85).aspx
	numEntries uint32
	table      [maxStateTableEntries]iphelperTCPRow
}

type iphelperTCPRow struct {
	// docs: https://msdn.microsoft.com/en-us/library/windows/desktop/aa366913(v=vs.85).aspx
	state      uint32
	localAddr  uint32
	localPort  uint32
	remoteAddr uint32
	remotePort uint32
	owningPid  uint32
}

type iphelperTCP6Table struct {
	// docs: https://msdn.microsoft.com/en-us/library/windows/desktop/aa366905(v=vs.85).aspx
	numEntries uint32
	table      [maxStateTableEntries]iphelperTCP6Row
}

type iphelperTCP6Row struct {
	// docs: https://msdn.microsoft.com/en-us/library/windows/desktop/aa366896(v=vs.85).aspx
	localAddr  [16]byte
	_          uint32 // localScopeID
	localPort  uint32
	remoteAddr [16]byte
	_          uint32 // remoteScopeID
	remotePort uint32
	state      uint32
	owningPid  uint32
}

type iphelperUDPTable struct {
	// docs: https://msdn.microsoft.com/en-us/library/windows/desktop/aa366932(v=vs.85).aspx
	numEntries uint32
	table      [maxStateTableEntries]iphelperUDPRow
}

type iphelperUDPRow struct {
	// docs: https://msdn.microsoft.com/en-us/library/windows/desktop/aa366928(v=vs.85).aspx
	localAddr uint32
	localPort uint32
	owningPid uint32
}

type iphelperUDP6Table struct {
	// docs: https://msdn.microsoft.com/en-us/library/windows/desktop/aa366925(v=vs.85).aspx
	numEntries uint32
	table      [maxStateTableEntries]iphelperUDP6Row
}

type iphelperUDP6Row struct {
	// docs: https://msdn.microsoft.com/en-us/library/windows/desktop/aa366923(v=vs.85).aspx
	localAddr [16]byte
	_         uint32 // localScopeID
	localPort uint32
	owningPid uint32
}

// IP and Protocol constants
const (
	IPv4 uint8 = 4
	IPv6 uint8 = 6

	TCP uint8 = 6
	UDP uint8 = 17
)

type learningBufSize struct {
	sync.Mutex

	size     int
	usesLeft int
	useFor   int
	start    int
	max      int
}

func newLearningBufSize(start, max, ttl int) *learningBufSize {
	return &learningBufSize{
		size:     start,
		usesLeft: ttl,
		useFor:   ttl,
		start:    start,
		max:      max,
	}
}

const (
	startBufSize = 1024

	// bufSizeUsageTTL defines how often a buffer size is used before it is
	// shrunk again.
	bufSizeUsageTTL = 100

	// maxBufSize is the maximum size we will allocate for responses. This was
	// previously set at 65k, which was too little for some production cases.
	maxBufSize = 1048576 // 2^20B, 1MB

	// maxStateTableEntries is the maximum supported amount of entries of the
	// state tables.
	// This is never allocated, but just casted to from an unsafe pointer.
	maxStateTableEntries = 65535
)

var (
	tcp4BufSize = newLearningBufSize(startBufSize, maxBufSize, bufSizeUsageTTL)
	udp4BufSize = newLearningBufSize(startBufSize, maxBufSize, bufSizeUsageTTL)
	tcp6BufSize = newLearningBufSize(startBufSize, maxBufSize, bufSizeUsageTTL)
	udp6BufSize = newLearningBufSize(startBufSize, maxBufSize, bufSizeUsageTTL)
)

func (lbf *learningBufSize) getBufSize() int {
	lbf.Lock()
	defer lbf.Unlock()

	// using bufSize
	lbf.usesLeft--
	// check if we want to reset
	if lbf.usesLeft <= 0 {
		// decrease
		lbf.size /= 2
		// not too little
		if lbf.size < lbf.start {
			lbf.size = lbf.start
		}
		// reset TTL counter
		lbf.usesLeft = lbf.useFor
	}

	return lbf.size
}

func (lbf *learningBufSize) increaseBufSize(minSize int) int {
	lbf.Lock()
	defer lbf.Unlock()

	// increase
	lbf.size *= 2
	// increase until we reach the minimum size
	for lbf.size < minSize {
		lbf.size *= 2
	}
	// not too much
	if lbf.size > lbf.max {
		lbf.size = lbf.max
	}
	// reset TTL counter
	lbf.usesLeft = lbf.useFor
	// return new bufSize
	return lbf.size
}

// getTable returns the current connection state table of Windows of the given protocol and IP version.
func (ipHelper *IPHelper) getTable(ipVersion, protocol uint8) (connections []*socket.ConnectionInfo, binds []*socket.BindInfo, err error) { //nolint:gocognit,gocycle // TODO
	// docs: https://docs.microsoft.com/en-us/windows/win32/api/iphlpapi/nf-iphlpapi-getextendedtcptable

	if !ipHelper.valid.IsSet() {
		return nil, nil, errInvalid
	}

	var afClass int
	var lbf *learningBufSize
	switch ipVersion {
	case IPv4:
		afClass = windows.AF_INET
		if protocol == TCP {
			lbf = tcp4BufSize
		} else {
			lbf = udp4BufSize
		}
	case IPv6:
		afClass = windows.AF_INET6
		if protocol == TCP {
			lbf = tcp6BufSize
		} else {
			lbf = udp6BufSize
		}
	default:
		return nil, nil, errors.New("invalid protocol")
	}

	// try max 5 times
	maxTries := 5
	usedBufSize := lbf.getBufSize()
	var buf []byte
triesLoop:
	for i := 1; i <= maxTries; i++ {
		bufSizeParam := usedBufSize
		buf = make([]byte, bufSizeParam)
		var r1 uintptr

		switch protocol {
		case TCP:
			r1, _, err = ipHelper.getExtendedTCPTable.Call(
				uintptr(unsafe.Pointer(&buf[0])),       // _Out_   PVOID           pTcpTable
				uintptr(unsafe.Pointer(&bufSizeParam)), // _Inout_ PDWORD          pdwSize
				0,                                      // _In_    BOOL            bOrder
				uintptr(afClass),                       // _In_    ULONG           ulAf
				iphelperTCPTableOwnerPIDAll,            // _In_    TCP_TABLE_CLASS TableClass
				0,                                      // _In_    ULONG           Reserved
			)
		case UDP:
			r1, _, err = ipHelper.getExtendedUDPTable.Call(
				uintptr(unsafe.Pointer(&buf[0])),       // _Out_   PVOID           pUdpTable,
				uintptr(unsafe.Pointer(&bufSizeParam)), // _Inout_ PDWORD          pdwSize,
				0,                                      // _In_    BOOL            bOrder,
				uintptr(afClass),                       // _In_    ULONG           ulAf,
				iphelperUDPTableOwnerPID,               // _In_    UDP_TABLE_CLASS TableClass,
				0,                                      // _In_    ULONG           Reserved
			)
		}

		switch r1 {
		case winErrInsufficientBuffer:
			if i >= maxTries {
				return nil, nil, fmt.Errorf(
					"insufficient buffer error (tried %d times): provided %d bytes; required %d bytes - [NT 0x%X] %s",
					i, usedBufSize, bufSizeParam, r1, err,
				)
			}
			// bufSizeParam was modified by ipHelper.getExtended*Table to hold the
			// required buffer size.
			usedBufSize = lbf.increaseBufSize(bufSizeParam)
		case winErrInvalidParameter:
			return nil, nil, fmt.Errorf("invalid parameter: [NT 0x%X] %s", r1, err)
		case windows.NO_ERROR:
			// success
			break triesLoop
		default:
			return nil, nil, fmt.Errorf("unexpected error: [NT 0x%X] %s", r1, err)
		}
	}

	// parse output
	switch {
	case protocol == TCP && ipVersion == IPv4:

		tcpTable := (*iphelperTCPTable)(unsafe.Pointer(&buf[0]))
		// Check if we got more entries than supported.
		tableEntries := tcpTable.numEntries
		if tableEntries > maxStateTableEntries {
			tableEntries = maxStateTableEntries
			log.Warningf("network/iphelper: received TCPv4 table with more entries than supported: %d/%d", tcpTable.numEntries, maxStateTableEntries)
		}
		// Cap table to actual entries.
		table := tcpTable.table[:tableEntries]

		for _, row := range table {
			if row.state == iphelperTCPStateListen {
				binds = append(binds, &socket.BindInfo{
					Local: socket.Address{
						IP:   convertIPv4(row.localAddr),
						Port: convertPort(row.localPort),
					},
					PID: int(row.owningPid),
				})
			} else {
				connections = append(connections, &socket.ConnectionInfo{
					Local: socket.Address{
						IP:   convertIPv4(row.localAddr),
						Port: convertPort(row.localPort),
					},
					Remote: socket.Address{
						IP:   convertIPv4(row.remoteAddr),
						Port: convertPort(row.remotePort),
					},
					PID: int(row.owningPid),
				})
			}
		}

	case protocol == TCP && ipVersion == IPv6:

		tcpTable := (*iphelperTCP6Table)(unsafe.Pointer(&buf[0]))
		// Check if we got more entries than supported.
		tableEntries := tcpTable.numEntries
		if tableEntries > maxStateTableEntries {
			tableEntries = maxStateTableEntries
			log.Warningf("network/iphelper: received TCPv6 table with more entries than supported: %d/%d", tcpTable.numEntries, maxStateTableEntries)
		}
		// Cap table to actual entries.
		table := tcpTable.table[:tableEntries]

		for _, row := range table {
			if row.state == iphelperTCPStateListen {
				binds = append(binds, &socket.BindInfo{
					Local: socket.Address{
						IP:   net.IP(row.localAddr[:]),
						Port: convertPort(row.localPort),
					},
					PID: int(row.owningPid),
				})
			} else {
				connections = append(connections, &socket.ConnectionInfo{
					Local: socket.Address{
						IP:   net.IP(row.localAddr[:]),
						Port: convertPort(row.localPort),
					},
					Remote: socket.Address{
						IP:   net.IP(row.remoteAddr[:]),
						Port: convertPort(row.remotePort),
					},
					PID: int(row.owningPid),
				})
			}
		}

	case protocol == UDP && ipVersion == IPv4:

		udpTable := (*iphelperUDPTable)(unsafe.Pointer(&buf[0]))
		// Check if we got more entries than supported.
		tableEntries := udpTable.numEntries
		if tableEntries > maxStateTableEntries {
			tableEntries = maxStateTableEntries
			log.Warningf("network/iphelper: received UDPv4 table with more entries than supported: %d/%d", udpTable.numEntries, maxStateTableEntries)
		}
		// Cap table to actual entries.
		table := udpTable.table[:tableEntries]

		for _, row := range table {
			binds = append(binds, &socket.BindInfo{
				Local: socket.Address{
					IP:   convertIPv4(row.localAddr),
					Port: convertPort(row.localPort),
				},
				PID: int(row.owningPid),
			})
		}

	case protocol == UDP && ipVersion == IPv6:

		udpTable := (*iphelperUDP6Table)(unsafe.Pointer(&buf[0]))
		// Check if we got more entries than supported.
		tableEntries := udpTable.numEntries
		if tableEntries > maxStateTableEntries {
			tableEntries = maxStateTableEntries
			log.Warningf("network/iphelper: received UDPv6 table with more entries than supported: %d/%d", udpTable.numEntries, maxStateTableEntries)
		}
		// Cap table to actual entries.
		table := udpTable.table[:tableEntries]

		for _, row := range table {
			binds = append(binds, &socket.BindInfo{
				Local: socket.Address{
					IP:   net.IP(row.localAddr[:]),
					Port: convertPort(row.localPort),
				},
				PID: int(row.owningPid),
			})
		}

	}

	return connections, binds, nil
}

// convertIPv4 as needed for iphlpapi.dll
func convertIPv4(input uint32) net.IP {
	addressBuf := make([]byte, 4)
	binary.LittleEndian.PutUint32(addressBuf, input)
	return net.IP(addressBuf)
}

// convertPort converts ports received from iphlpapi.dll
func convertPort(input uint32) uint16 {
	return uint16(input>>8 | input<<8)
}
