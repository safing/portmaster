// Copyright Safing ICS Technologies GmbH. Use of this source code is governed by the AGPL license that can be found in the LICENSE file.

package proc

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"github.com/Safing/portbase/log"
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

	TCP_ESTABLISHED = iota + 1
	TCP_SYN_SENT
	TCP_SYN_RECV
	TCP_FIN_WAIT1
	TCP_FIN_WAIT2
	TCP_TIME_WAIT
	TCP_CLOSE
	TCP_CLOSE_WAIT
	TCP_LAST_ACK
	TCP_LISTEN
	TCP_CLOSING
	TCP_NEW_SYN_RECV
)

var (
	// connectionSocketsLock sync.Mutex
	// connectionTCP4 = make(map[string][]int)
	// connectionUDP4 = make(map[string][]int)
	// connectionTCP6 = make(map[string][]int)
	// connectionUDP6 = make(map[string][]int)

	listeningSocketsLock sync.Mutex
	addressListeningTCP4 = make(map[string][]int)
	addressListeningUDP4 = make(map[string][]int)
	addressListeningTCP6 = make(map[string][]int)
	addressListeningUDP6 = make(map[string][]int)
	globalListeningTCP4  = make(map[uint16][]int)
	globalListeningUDP4  = make(map[uint16][]int)
	globalListeningTCP6  = make(map[uint16][]int)
	globalListeningUDP6  = make(map[uint16][]int)
)

func getConnectionSocket(localIP *net.IP, localPort uint16, protocol uint8) (int, int, bool) {
	// listeningSocketsLock.Lock()
	// defer listeningSocketsLock.Unlock()

	var procFile string
	var localIPHex string
	switch protocol {
	case TCP4:
		procFile = TCP4Data
		localIPBytes := []byte(localIP.To4())
		localIPHex = strings.ToUpper(hex.EncodeToString([]byte{localIPBytes[3], localIPBytes[2], localIPBytes[1], localIPBytes[0]}))
	case UDP4:
		procFile = UDP4Data
		localIPBytes := []byte(localIP.To4())
		localIPHex = strings.ToUpper(hex.EncodeToString([]byte{localIPBytes[3], localIPBytes[2], localIPBytes[1], localIPBytes[0]}))
	case TCP6:
		procFile = TCP6Data
		localIPHex = hex.EncodeToString([]byte(*localIP))
	case UDP6:
		procFile = UDP6Data
		localIPHex = hex.EncodeToString([]byte(*localIP))
	}

	localPortHex := fmt.Sprintf("%04X", localPort)

	// log.Tracef("process/proc: searching for PID of: %s:%d (%s:%s)", localIP, localPort, localIPHex, localPortHex)

	// open file
	socketData, err := os.Open(procFile)
	if err != nil {
		log.Warningf("process/proc: could not read %s: %s", procFile, err)
		return -1, -1, false
	}
	defer socketData.Close()

	// file scanner
	scanner := bufio.NewScanner(socketData)
	scanner.Split(bufio.ScanLines)

	// parse
	scanner.Scan() // skip first line
	for scanner.Scan() {
		line := strings.FieldsFunc(scanner.Text(), procDelimiter)
		// log.Tracef("line: %s", line)
		if len(line) < 14 {
			// log.Tracef("process: too short: %s", line)
			continue
		}

		if line[1] != localIPHex {
			continue
		}
		if line[2] != localPortHex {
			continue
		}

		ok := true

		uid, err := strconv.ParseInt(line[11], 10, 32)
		if err != nil {
			log.Warningf("process: could not parse uid %s: %s", line[11], err)
			uid = -1
			ok = false
		}

		inode, err := strconv.ParseInt(line[13], 10, 32)
		if err != nil {
			log.Warningf("process: could not parse inode %s: %s", line[13], err)
			inode = -1
			ok = false
		}

		// log.Tracef("process/proc: identified process of %s:%d: socket=%d uid=%d", localIP, localPort, int(inode), int(uid))
		return int(uid), int(inode), ok

	}

	return -1, -1, false

}

func getListeningSocket(localIP *net.IP, localPort uint16, protocol uint8) (uid, inode int, ok bool) {
	listeningSocketsLock.Lock()
	defer listeningSocketsLock.Unlock()

	var addressListening *map[string][]int
	var globalListening *map[uint16][]int
	switch protocol {
	case TCP4:
		addressListening = &addressListeningTCP4
		globalListening = &globalListeningTCP4
	case UDP4:
		addressListening = &addressListeningUDP4
		globalListening = &globalListeningUDP4
	case TCP6:
		addressListening = &addressListeningTCP6
		globalListening = &globalListeningTCP6
	case UDP6:
		addressListening = &addressListeningUDP6
		globalListening = &globalListeningUDP6
	}

	data, ok := (*addressListening)[fmt.Sprintf("%s:%d", localIP, localPort)]
	if !ok {
		data, ok = (*globalListening)[localPort]
	}
	if ok {
		return data[0], data[1], true
	}
	updateListeners(protocol)
	data, ok = (*addressListening)[fmt.Sprintf("%s:%d", localIP, localPort)]
	if !ok {
		data, ok = (*globalListening)[localPort]
	}
	if ok {
		return data[0], data[1], true
	}

	return 0, 0, false
}

func procDelimiter(c rune) bool {
	return unicode.IsSpace(c) || c == ':'
}

func convertIPv4(data string) *net.IP {
	decoded, err := hex.DecodeString(data)
	if err != nil {
		log.Warningf("process: could not parse IPv4 %s: %s", data, err)
		return nil
	}
	if len(decoded) != 4 {
		log.Warningf("process: decoded IPv4 %s has wrong length")
		return nil
	}
	ip := net.IPv4(decoded[3], decoded[2], decoded[1], decoded[0])
	return &ip
}

func convertIPv6(data string) *net.IP {
	decoded, err := hex.DecodeString(data)
	if err != nil {
		log.Warningf("process: could not parse IPv6 %s: %s", data, err)
		return nil
	}
	if len(decoded) != 16 {
		log.Warningf("process: decoded IPv6 %s has wrong length")
		return nil
	}
	ip := net.IP(decoded)
	return &ip
}

func updateListeners(protocol uint8) {
	switch protocol {
	case TCP4:
		addressListeningTCP4, globalListeningTCP4 = getListenerMaps(TCP4Data, "00000000", "0A", convertIPv4)
	case UDP4:
		addressListeningUDP4, globalListeningUDP4 = getListenerMaps(UDP4Data, "00000000", "07", convertIPv4)
	case TCP6:
		addressListeningTCP6, globalListeningTCP6 = getListenerMaps(TCP6Data, "00000000000000000000000000000000", "0A", convertIPv6)
	case UDP6:
		addressListeningUDP6, globalListeningUDP6 = getListenerMaps(UDP6Data, "00000000000000000000000000000000", "07", convertIPv6)
	}
}

func getListenerMaps(procFile, zeroIP, socketStatusListening string, ipConverter func(string) *net.IP) (map[string][]int, map[uint16][]int) {
	addressListening := make(map[string][]int)
	globalListening := make(map[uint16][]int)

	// open file
	socketData, err := os.Open(procFile)
	if err != nil {
		log.Warningf("process: could not read %s: %s", procFile, err)
		return addressListening, globalListening
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
		if line[5] != socketStatusListening {
			// skip if not listening
			// log.Tracef("process: not listening %s: %s", line, line[5])
			continue
		}

		port, err := strconv.ParseUint(line[2], 16, 16)
		// log.Tracef("port: %s", line[2])
		if err != nil {
			log.Warningf("process: could not parse port %s: %s", line[2], err)
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

		if line[1] == zeroIP {
			globalListening[uint16(port)] = []int{int(uid), int(inode)}
		} else {
			address := ipConverter(line[1])
			if address != nil {
				addressListening[fmt.Sprintf("%s:%d", address, port)] = []int{int(uid), int(inode)}
			}
		}

	}

	return addressListening, globalListening
}

func GetActiveConnectionIDs() []string {
	var connections []string

	connections = append(connections, getConnectionIDsFromSource(TCP4Data, 6, convertIPv4)...)
	connections = append(connections, getConnectionIDsFromSource(UDP4Data, 17, convertIPv4)...)
	connections = append(connections, getConnectionIDsFromSource(TCP6Data, 6, convertIPv6)...)
	connections = append(connections, getConnectionIDsFromSource(UDP6Data, 17, convertIPv6)...)

	return connections
}

func getConnectionIDsFromSource(source string, protocol uint16, ipConverter func(string) *net.IP) []string {
	var connections []string

	// open file
	socketData, err := os.Open(source)
	if err != nil {
		log.Warningf("process: could not read %s: %s", source, err)
		return connections
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

		// skip listeners and closed connections
		if line[5] == "0A" || line[5] == "07" {
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

		remoteIP := ipConverter(line[3])
		if remoteIP == nil {
			continue
		}

		remotePort, err := strconv.ParseUint(line[4], 16, 16)
		if err != nil {
			log.Warningf("process: could not parse port: %s", err)
			continue
		}

		connections = append(connections, fmt.Sprintf("%d-%s-%d-%s-%d", protocol, localIP, localPort, remoteIP, remotePort))
	}

	return connections
}
