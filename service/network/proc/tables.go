//go:build linux

package proc

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"unicode"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/network/socket"
)

/*

1. find socket inode
  - by incoming (listenting sockets) or outgoing (local port + external IP + port) - also local IP?
  - /proc/net/{tcp|udp}[6]

2. get list of processes of uid

3. find socket inode in process fds
	- if not found, refresh map of uid->pids
	- if not found, check ALL pids: maybe euid != uid

4. gather process info

Cache every step!

*/

// Network Related Constants.
const (
	TCP4 uint8 = iota
	UDP4
	TCP6
	UDP6
	ICMP4
	ICMP6

	tcp4ProcFile = "/proc/net/tcp"
	tcp6ProcFile = "/proc/net/tcp6"
	udp4ProcFile = "/proc/net/udp"
	udp6ProcFile = "/proc/net/udp6"

	tcpListenStateHex = "0A"
)

// GetTCP4Table returns the system table for IPv4 TCP activity.
func GetTCP4Table() (connections []*socket.ConnectionInfo, listeners []*socket.BindInfo, err error) {
	return getTableFromSource(TCP4, tcp4ProcFile)
}

// GetTCP6Table returns the system table for IPv6 TCP activity.
func GetTCP6Table() (connections []*socket.ConnectionInfo, listeners []*socket.BindInfo, err error) {
	return getTableFromSource(TCP6, tcp6ProcFile)
}

// GetUDP4Table returns the system table for IPv4 UDP activity.
func GetUDP4Table() (binds []*socket.BindInfo, err error) {
	_, binds, err = getTableFromSource(UDP4, udp4ProcFile)
	return
}

// GetUDP6Table returns the system table for IPv6 UDP activity.
func GetUDP6Table() (binds []*socket.BindInfo, err error) {
	_, binds, err = getTableFromSource(UDP6, udp6ProcFile)
	return
}

const (
	// hint: we split fields by multiple delimiters, see procDelimiter
	fieldIndexLocalIP    = 1
	fieldIndexLocalPort  = 2
	fieldIndexRemoteIP   = 3
	fieldIndexRemotePort = 4
	fieldIndexUID        = 11
	fieldIndexInode      = 13
)

func getTableFromSource(stack uint8, procFile string) (connections []*socket.ConnectionInfo, binds []*socket.BindInfo, err error) {
	var ipConverter func(string) net.IP
	switch stack {
	case TCP4, UDP4:
		ipConverter = convertIPv4
	case TCP6, UDP6:
		ipConverter = convertIPv6
	default:
		return nil, nil, fmt.Errorf("unsupported table stack: %d", stack)
	}

	// open file
	socketData, err := os.Open(procFile)
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		_ = socketData.Close()
	}()

	// file scanner
	scanner := bufio.NewScanner(socketData)
	scanner.Split(bufio.ScanLines)

	// parse
	scanner.Scan() // skip first row
	for scanner.Scan() {
		fields := strings.FieldsFunc(scanner.Text(), procDelimiter)
		if len(fields) < 14 {
			// log.Tracef("proc: too short: %s", fields)
			continue
		}

		localIP := ipConverter(fields[fieldIndexLocalIP])
		if localIP == nil {
			continue
		}

		localPort, err := strconv.ParseUint(fields[fieldIndexLocalPort], 16, 16)
		if err != nil {
			log.Warningf("proc: could not parse port: %s", err)
			continue
		}

		uid, err := strconv.ParseInt(fields[fieldIndexUID], 10, 32)
		// log.Tracef("uid: %s", fields[fieldIndexUID])
		if err != nil {
			log.Warningf("proc: could not parse uid %s: %s", fields[11], err)
			continue
		}

		inode, err := strconv.ParseInt(fields[fieldIndexInode], 10, 32)
		// log.Tracef("inode: %s", fields[fieldIndexInode])
		if err != nil {
			log.Warningf("proc: could not parse inode %s: %s", fields[13], err)
			continue
		}

		switch stack {
		case UDP4, UDP6:

			binds = append(binds, &socket.BindInfo{
				Local: socket.Address{
					IP:   localIP,
					Port: uint16(localPort),
				},
				PID:   socket.UndefinedProcessID,
				UID:   int(uid),
				Inode: int(inode),
			})

		case TCP4, TCP6:

			if fields[5] == tcpListenStateHex {
				// listener

				binds = append(binds, &socket.BindInfo{
					Local: socket.Address{
						IP:   localIP,
						Port: uint16(localPort),
					},
					PID:   socket.UndefinedProcessID,
					UID:   int(uid),
					Inode: int(inode),
				})
			} else {
				// connection

				remoteIP := ipConverter(fields[fieldIndexRemoteIP])
				if remoteIP == nil {
					continue
				}

				remotePort, err := strconv.ParseUint(fields[fieldIndexRemotePort], 16, 16)
				if err != nil {
					log.Warningf("proc: could not parse port: %s", err)
					continue
				}

				connections = append(connections, &socket.ConnectionInfo{
					Local: socket.Address{
						IP:   localIP,
						Port: uint16(localPort),
					},
					Remote: socket.Address{
						IP:   remoteIP,
						Port: uint16(remotePort),
					},
					PID:   socket.UndefinedProcessID,
					UID:   int(uid),
					Inode: int(inode),
				})
			}
		}
	}

	return connections, binds, nil
}

func procDelimiter(c rune) bool {
	return unicode.IsSpace(c) || c == ':'
}

func convertIPv4(data string) net.IP {
	// Decode and bullshit check the data length.
	decoded, err := hex.DecodeString(data)
	if err != nil {
		log.Warningf("proc: could not parse IPv4 %s: %s", data, err)
		return nil
	}
	if len(decoded) != 4 {
		log.Warningf("proc: decoded IPv4 %s has wrong length", decoded)
		return nil
	}

	// Build the IPv4 address with the reversed byte order.
	ip := net.IPv4(decoded[3], decoded[2], decoded[1], decoded[0])
	return ip
}

func convertIPv6(data string) net.IP {
	// Decode and bullshit check the data length.
	decoded, err := hex.DecodeString(data)
	if err != nil {
		log.Warningf("proc: could not parse IPv6 %s: %s", data, err)
		return nil
	}
	if len(decoded) != 16 {
		log.Warningf("proc: decoded IPv6 %s has wrong length", decoded)
		return nil
	}

	// Build the IPv6 address with the translated byte order.
	for i := 0; i < 16; i += 4 {
		decoded[i], decoded[i+1], decoded[i+2], decoded[i+3] = decoded[i+3], decoded[i+2], decoded[i+1], decoded[i]
	}
	ip := net.IP(decoded)
	return ip
}
