// +build windows

package iphelper

import (
	"errors"
	"fmt"
	"net"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	iphelper_TCP_TABLE_OWNER_PID_ALL uintptr = 5
	iphelper_UDP_TABLE_OWNER_PID     uintptr = 1
	iphelper_TCP_STATE_LISTEN        uint32  = 2
)

type connectionEntry struct {
	localIP    net.IP
	remoteIP   net.IP
	localPort  uint16
	remotePort uint16
	pid        int
}

func (entry *connectionEntry) String() string {
	return fmt.Sprintf("PID=%d %s:%d <> %s:%d", entry.pid, entry.localIP, entry.localPort, entry.remoteIP, entry.remotePort)
}

type iphelperTcpTable struct {
	// docs: https://msdn.microsoft.com/en-us/library/windows/desktop/aa366921(v=vs.85).aspx
	numEntries uint32
	table      [4096]iphelperTcpRow
}

type iphelperTcpRow struct {
	// docs: https://msdn.microsoft.com/en-us/library/windows/desktop/aa366913(v=vs.85).aspx
	state      uint32
	localAddr  uint32
	localPort  uint32
	remoteAddr uint32
	remotePort uint32
	owningPid  uint32
}

type iphelperTcp6Table struct {
	// docs: https://msdn.microsoft.com/en-us/library/windows/desktop/aa366905(v=vs.85).aspx
	numEntries uint32
	table      [4096]iphelperTcp6Row
}

type iphelperTcp6Row struct {
	// docs: https://msdn.microsoft.com/en-us/library/windows/desktop/aa366896(v=vs.85).aspx
	localAddr     [16]byte
	localScopeId  uint32
	localPort     uint32
	remoteAddr    [16]byte
	remoteScopeId uint32
	remotePort    uint32
	state         uint32
	owningPid     uint32
}

type iphelperUdpTable struct {
	// docs: https://msdn.microsoft.com/en-us/library/windows/desktop/aa366932(v=vs.85).aspx
	numEntries uint32
	table      [4096]iphelperUdpRow
}

type iphelperUdpRow struct {
	// docs: https://msdn.microsoft.com/en-us/library/windows/desktop/aa366928(v=vs.85).aspx
	localAddr uint32
	localPort uint32
	owningPid uint32
}

type iphelperUdp6Table struct {
	// docs: https://msdn.microsoft.com/en-us/library/windows/desktop/aa366925(v=vs.85).aspx
	numEntries uint32
	table      [4096]iphelperUdp6Row
}

type iphelperUdp6Row struct {
	// docs: https://msdn.microsoft.com/en-us/library/windows/desktop/aa366923(v=vs.85).aspx
	localAddr    [16]byte
	localScopeId uint32
	localPort    uint32
	owningPid    uint32
}

const (
	IPv4 uint8 = 4
	IPv6 uint8 = 6

	TCP uint8 = 6
	UDP uint8 = 17
)

func (ipHelper *IPHelper) GetTables(protocol uint8, ipVersion uint8) (connections []*connectionEntry, listeners []*connectionEntry, err error) {
	// docs: https://msdn.microsoft.com/en-us/library/windows/desktop/aa365928(v=vs.85).aspx

	if !ipHelper.valid.IsSet() {
		return nil, nil, errInvalid
	}

	var afClass int
	switch ipVersion {
	case IPv4:
		afClass = windows.AF_INET
	case IPv6:
		afClass = windows.AF_INET6
	default:
		return nil, nil, errors.New("invalid protocol")
	}

	bufSize := 4096
	buf := make([]byte, bufSize)
	var r1 uintptr

	switch protocol {
	case TCP:
		r1, _, err = ipHelper.getExtendedTcpTable.Call(
			uintptr(unsafe.Pointer(&buf[0])),  // _Out_   PVOID           pTcpTable
			uintptr(unsafe.Pointer(&bufSize)), // _Inout_ PDWORD          pdwSize
			0,                                 // _In_    BOOL            bOrder
			uintptr(afClass),                  // _In_    ULONG           ulAf
			iphelper_TCP_TABLE_OWNER_PID_ALL,  // _In_    TCP_TABLE_CLASS TableClass
			0,                                 // _In_    ULONG           Reserved
		)
	case UDP:
		r1, _, err = ipHelper.getExtendedUdpTable.Call(
			uintptr(unsafe.Pointer(&buf[0])),  // _Out_   PVOID           pUdpTable,
			uintptr(unsafe.Pointer(&bufSize)), // _Inout_ PDWORD          pdwSize,
			0,                                 // _In_    BOOL            bOrder,
			uintptr(afClass),                  // _In_    ULONG           ulAf,
			iphelper_UDP_TABLE_OWNER_PID,      // _In_    UDP_TABLE_CLASS TableClass,
			0,                                 // _In_    ULONG           Reserved
		)
	}

	switch r1 {
	// case windows.ERROR_INSUFFICIENT_BUFFER:
	// 	return nil, fmt.Errorf("insufficient buffer error: %s", err)
	// case windows.ERROR_INVALID_PARAMETER:
	// 	return nil, fmt.Errorf("invalid parameter: %s", err)
	case windows.NO_ERROR:
	default:
		return nil, nil, fmt.Errorf("unexpected error: %s", err)
	}

	// parse output
	switch {
	case protocol == TCP && ipVersion == IPv4:

		tcpTable := (*iphelperTcpTable)(unsafe.Pointer(&buf[0]))
		table := tcpTable.table[:tcpTable.numEntries]

		for _, row := range table {
			new := &connectionEntry{}

			// PID
			new.pid = int(row.owningPid)

			// local
			if row.localAddr != 0 {
				new.localIP = convertIPv4(row.localAddr)
			}
			new.localPort = uint16(row.localPort>>8 | row.localPort<<8)

			// remote
			if row.state == iphelper_TCP_STATE_LISTEN {
				listeners = append(listeners, new)
			} else {
				new.remoteIP = convertIPv4(row.remoteAddr)
				new.remotePort = uint16(row.remotePort>>8 | row.remotePort<<8)
				connections = append(connections, new)
			}

		}

	case protocol == TCP && ipVersion == IPv6:

		tcpTable := (*iphelperTcp6Table)(unsafe.Pointer(&buf[0]))
		table := tcpTable.table[:tcpTable.numEntries]

		for _, row := range table {
			new := &connectionEntry{}

			// PID
			new.pid = int(row.owningPid)

			// local
			new.localIP = net.IP(row.localAddr[:])
			new.localPort = uint16(row.localPort>>8 | row.localPort<<8)

			// remote
			if row.state == iphelper_TCP_STATE_LISTEN {
				if new.localIP.Equal(net.IPv6zero) {
					new.localIP = nil
				}
				listeners = append(listeners, new)
			} else {
				new.remoteIP = net.IP(row.remoteAddr[:])
				new.remotePort = uint16(row.remotePort>>8 | row.remotePort<<8)
				connections = append(connections, new)
			}

		}

	case protocol == UDP && ipVersion == IPv4:

		udpTable := (*iphelperUdpTable)(unsafe.Pointer(&buf[0]))
		table := udpTable.table[:udpTable.numEntries]

		for _, row := range table {
			new := &connectionEntry{}

			// PID
			new.pid = int(row.owningPid)

			// local
			new.localPort = uint16(row.localPort>>8 | row.localPort<<8)
			if row.localAddr == 0 {
				listeners = append(listeners, new)
			} else {
				new.localIP = convertIPv4(row.localAddr)
				connections = append(connections, new)
			}
		}

	case protocol == UDP && ipVersion == IPv6:

		udpTable := (*iphelperUdp6Table)(unsafe.Pointer(&buf[0]))
		table := udpTable.table[:udpTable.numEntries]

		for _, row := range table {
			new := &connectionEntry{}

			// PID
			new.pid = int(row.owningPid)

			// local
			new.localIP = net.IP(row.localAddr[:])
			new.localPort = uint16(row.localPort>>8 | row.localPort<<8)
			if new.localIP.Equal(net.IPv6zero) {
				new.localIP = nil
				listeners = append(listeners, new)
			} else {
				connections = append(connections, new)
			}
		}

	}

	return connections, listeners, nil
}

func convertIPv4(input uint32) net.IP {
	return net.IPv4(
		uint8(input&0xFF),
		uint8(input>>8&0xFF),
		uint8(input>>16&0xFF),
		uint8(input>>24&0xFF),
	)
}
