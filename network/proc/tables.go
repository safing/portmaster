// +build linux

package proc

import (
	"bufio"
	"encoding/hex"
	"net"
	"os"
	"strconv"
	"strings"
	"unicode"

	"github.com/safing/portmaster/network/socket"

	"github.com/safing/portbase/log"
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

// Network Related Constants
const (
	TCP4 uint8 = iota
	UDP4
	TCP6
	UDP6
	ICMP4
	ICMP6

	TCP4Data  = "/proc/net/tcp"
	UDP4Data  = "/proc/net/udp"
	TCP6Data  = "/proc/net/tcp6"
	UDP6Data  = "/proc/net/udp6"
	ICMP4Data = "/proc/net/icmp"
	ICMP6Data = "/proc/net/icmp6"

	UnfetchedProcessID = -2

	tcpListenStateHex = "0A"
)

// GetTCP4Table returns the system table for IPv4 TCP activity.
func GetTCP4Table() (connections []*socket.ConnectionInfo, listeners []*socket.BindInfo, err error) {
	return getTableFromSource(TCP4, TCP4Data, convertIPv4)
}

// GetTCP6Table returns the system table for IPv6 TCP activity.
func GetTCP6Table() (connections []*socket.ConnectionInfo, listeners []*socket.BindInfo, err error) {
	return getTableFromSource(TCP6, TCP6Data, convertIPv6)
}

// GetUDP4Table returns the system table for IPv4 UDP activity.
func GetUDP4Table() (binds []*socket.BindInfo, err error) {
	_, binds, err = getTableFromSource(UDP4, UDP4Data, convertIPv4)
	return
}

// GetUDP6Table returns the system table for IPv6 UDP activity.
func GetUDP6Table() (binds []*socket.BindInfo, err error) {
	_, binds, err = getTableFromSource(UDP6, UDP6Data, convertIPv6)
	return
}

func getTableFromSource(stack uint8, procFile string, ipConverter func(string) net.IP) (connections []*socket.ConnectionInfo, binds []*socket.BindInfo, err error) {

	// open file
	socketData, err := os.Open(procFile)
	if err != nil {
		return nil, nil, err
	}
	defer socketData.Close()

	// file scanner
	scanner := bufio.NewScanner(socketData)
	scanner.Split(bufio.ScanLines)

	// parse
	scanner.Scan() // skip first line
	for scanner.Scan() {
		line := strings.FieldsFunc(scanner.Text(), procDelimiter)
		if len(line) < 14 {
			// log.Tracef("process: too short: %s", line)
			continue
		}

		localIP := ipConverter(line[1])
		if localIP == nil {
			continue
		}

		localPort, err := strconv.ParseUint(line[2], 16, 16)
		if err != nil {
			log.Warningf("process: could not parse port: %s", err)
			continue
		}

		uid, err := strconv.ParseInt(line[11], 10, 32)
		// log.Tracef("uid: %s", line[11])
		if err != nil {
			log.Warningf("process: could not parse uid %s: %s", line[11], err)
			continue
		}

		inode, err := strconv.ParseInt(line[13], 10, 32)
		// log.Tracef("inode: %s", line[13])
		if err != nil {
			log.Warningf("process: could not parse inode %s: %s", line[13], err)
			continue
		}

		switch stack {
		case UDP4, UDP6:

			binds = append(binds, &socket.BindInfo{
				Local: socket.Address{
					IP:   localIP,
					Port: uint16(localPort),
				},
				PID:   UnfetchedProcessID,
				UID:   int(uid),
				Inode: int(inode),
			})

		case TCP4, TCP6:

			if line[5] == tcpListenStateHex {
				// listener

				binds = append(binds, &socket.BindInfo{
					Local: socket.Address{
						IP:   localIP,
						Port: uint16(localPort),
					},
					PID:   UnfetchedProcessID,
					UID:   int(uid),
					Inode: int(inode),
				})
			} else {
				// connection

				remoteIP := ipConverter(line[3])
				if remoteIP == nil {
					continue
				}

				remotePort, err := strconv.ParseUint(line[4], 16, 16)
				if err != nil {
					log.Warningf("process: could not parse port: %s", err)
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
					PID:   UnfetchedProcessID,
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
	decoded, err := hex.DecodeString(data)
	if err != nil {
		log.Warningf("process: could not parse IPv4 %s: %s", data, err)
		return nil
	}
	if len(decoded) != 4 {
		log.Warningf("process: decoded IPv4 %s has wrong length", decoded)
		return nil
	}
	ip := net.IPv4(decoded[3], decoded[2], decoded[1], decoded[0])
	return ip
}

func convertIPv6(data string) net.IP {
	decoded, err := hex.DecodeString(data)
	if err != nil {
		log.Warningf("process: could not parse IPv6 %s: %s", data, err)
		return nil
	}
	if len(decoded) != 16 {
		log.Warningf("process: decoded IPv6 %s has wrong length", decoded)
		return nil
	}
	ip := net.IP(decoded)
	return ip
}
