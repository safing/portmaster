package portscan

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/safing/portbase/log"
	"github.com/safing/portbase/modules"
	"github.com/safing/portmaster/firewall/inspection"
	"github.com/safing/portmaster/netenv"
	"github.com/safing/portmaster/network"
	"github.com/safing/portmaster/network/packet"
	"github.com/safing/portmaster/process"
	"github.com/safing/portmaster/status"
)

type tcpUDPport struct {
	protocol packet.IPProtocol
	port     uint16
}

type ipData struct {
	score int // score needs to be big enough to keep maxScore + addScore... to prevent overflow
	//	greylistingWorked bool
	previousOffender bool
	blocked          bool
	ignore           bool
	lastSeen         time.Time
	lastUpdated      time.Time
	blockedPorts     []tcpUDPport
}

const (
	cleanUpInterval = 5 * time.Minute
	cleanUpMaxDelay = 5 * time.Minute

	decreaseInterval      = 11 * time.Second
	unblockIdleTime       = 1 * time.Hour
	undoSuspicionIdleTime = 24 * time.Hour
	unignoreTime          = 24 * time.Hour

	registeredPortsStart = 1024
	dynamicPortsStart    = 32768

	addScoreWellKnownPort  = 40
	addScoreRegisteredPort = 20
	addScoreDynamicPort    = 10

	scoreBlock = 160
	maxScore   = 320

	threatIDPrefix = "portscan:"
)

var (
	ips map[string]*ipData

	module        *modules.Module
	detectorMutex sync.Mutex
)

// Detector detects if a connection is part of a portscan which already sent some packets.
type Detector struct{}

// Name implements the inspection interface.
func (d *Detector) Name() string {
	return "Portscan Detection"
}

// Inspect implements the inspection interface.
func (d *Detector) Inspect(conn *network.Connection, pkt packet.Packet) (network.Verdict, bool, error) {
	detectorMutex.Lock()
	defer detectorMutex.Unlock()

	ctx := pkt.Ctx()

	log.Tracer(ctx).Debugf("portscan-detection: new connection")

	rIP, ok := conn.Entity.GetIP() // remote IP
	if !ok {                       // No IP => return undecided
		return network.VerdictUndecided, false, nil
	}

	ipString := conn.LocalIP.String() + "-" + rIP.String() //localip-remoteip
	entry, inMap := ips[ipString]

	log.Tracer(ctx).Debugf("portscan-detection: Conn: %s, remotePort: %d, IP: %s, Protocol: %s, LocalIP: %s, LocalPort: %d, inMap: %t, entry: %s", conn, conn.Entity.Port, conn.Entity.IP, conn.IPProtocol, conn.LocalIP, conn.LocalPort, inMap, entry)

	if inMap {
		inMap = entry.updateIPstate(ipString) // needs to be run before updating lastSeen (lastUpdated is updated within)
	}

	if inMap {
		entry.lastSeen = time.Now()

		if entry.ignore {
			return network.VerdictUndecided, false, nil
		}
	}

	proc := conn.Process()
	myip, _ := netenv.IsMyIP(conn.LocalIP)

	// malicious Packet? This if checks all conditions for a malicious packet
	switch {
	case proc != nil && proc.Pid != process.UnidentifiedProcessID:
		//We don't handle connections to running apps
	case !conn.Inbound:
		//We don't handle outbound connections
	case !(conn.IPProtocol == packet.TCP || conn.IPProtocol == packet.UDP):
		//We only handle TCP and UDP
	case !myip:
		//we only handle connections to our own IP
	case isNetBIOSoverTCPIP(conn):
		//we currently ignore NetBIOS
	case (conn.IPProtocol == packet.UDP && (conn.LocalPort == 67 || conn.LocalPort == 68)):
		//we ignore DHCP
	default:
		//We count this packet as a malicious packet
		handleMaliciousPacket(ctx, inMap, conn, entry, ipString)
	}

	if inMap && entry.blocked {
		log.Tracer(ctx).Debugf("portscan-detection: blocking")
		conn.SetVerdict(network.VerdictDrop, "Portscan", nil)
	} else {
		log.Tracer(ctx).Debugf("portscan-detection: let through")
	}

	return network.VerdictUndecided, false, nil // If dropped, the whole connection is already dropped by conn.SetVerdict above
}

func handleMaliciousPacket(ctx context.Context, inMap bool, conn *network.Connection, entry *ipData, ipString string) {
	// define Portscore
	var addScore int
	switch {
	case conn.LocalPort < registeredPortsStart:
		addScore = addScoreWellKnownPort
	case conn.LocalPort < dynamicPortsStart:
		addScore = addScoreRegisteredPort
	default:
		addScore = addScoreDynamicPort
	}

	port := tcpUDPport{protocol: conn.IPProtocol, port: conn.LocalPort}

	if !inMap {
		// new IP => add to List
		ips[ipString] = &ipData{
			score:        addScore,
			blockedPorts: []tcpUDPport{port},
			lastSeen:     time.Now(),
			lastUpdated:  time.Now(),
		}
		log.Tracer(ctx).Debugf("portscan-detection: New Entry: %s", ips[ipString])
		return
	}

	// the Port in list of tried ports - otherwise it would have already returned
	triedPort := false
	for _, e := range entry.blockedPorts {
		if e == port {
			triedPort = true
			break
		}
	}

	if !triedPort {
		entry.blockedPorts = append(entry.blockedPorts, port)
		entry.score = intMin(entry.score+addScore, maxScore)

		if entry.previousOffender || entry.score >= scoreBlock {
			entry.blocked = true
			entry.previousOffender = true

			// FIXME: actually I just want to know if THIS threat exists - I don't need prefixing. Maybe we can do it simpler ... (less CPU-intensive)
			if t, _ := status.GetThreats(threatIDPrefix + ipString); len(t) == 0 {
				log.Tracer(ctx).Infof("portscan-detection: new Threat %s", extractRemoteFromIPString(ipString))
				status.AddOrUpdateThreat(&status.Threat{
					ID:              threatIDPrefix + ipString,
					Name:            "Detected portscan from " + extractRemoteFromIPString(ipString),
					Description:     "The device with the IP address " + extractRemoteFromIPString(ipString) + " is scanning network ports on your device.",
					MitigationLevel: status.SecurityLevelHigh,
					Started:         time.Now().Unix(),
				})
			}
		}
	}

	log.Tracer(ctx).Debugf("portscan-detection: changed Entry: %s", entry)
}

// updateIPstate updates this 4 Values of the Struct
// ipString needs to correspond to the key of the entry in the map ips
// needs to be run before updating lastSeen (lastUpdated is updated within)
// WARNING: This function maybe deletes the entry ipString from the Map ips. (look at the returncode)
// return: still in map? (bool)
func (ip *ipData) updateIPstate(ipString string) bool {
	ip.score -= intMin(int(time.Since(ip.lastUpdated)/decreaseInterval), ip.score)

	if ip.ignore {
		if time.Since(ip.lastSeen) > unignoreTime {
			ip.ignore = false
		}
	}

	if ip.previousOffender && time.Since(ip.lastSeen) > undoSuspicionIdleTime {
		ip.previousOffender = false
	}

	if ip.blocked && time.Since(ip.lastSeen) > unblockIdleTime {
		ip.blocked = false
		ip.blockedPorts = []tcpUDPport{}

		status.DeleteThreat(threatIDPrefix + ipString)
	}

	ip.lastUpdated = time.Now()

	if !ip.blocked && !ip.ignore && !ip.previousOffender && ip.score == 0 {
		delete(ips, ipString)
		return false
	}

	return true
}

// Destroy implements the destroy interface.
func (d *Detector) Destroy() error {
	return nil
}

// DetectorFactory creates&returns a detector for a connection
func DetectorFactory(conn *network.Connection, pkt packet.Packet) (network.Inspector, error) {
	return &Detector{}, nil
}

// Register registers the encryption detection inspector with the inspection framework.
func init() {
	module = modules.Register("portscan-detection", nil, start, nil, "base", "netenv")
	module.Enable() // FIXME
}

func updateWholeList() {
	log.Debugf("portscan-detection: update list&cleanup")

	detectorMutex.Lock()
	defer detectorMutex.Unlock()

	for ip, entry := range ips {

		if entry.updateIPstate(ip) {
			log.Debugf("portscan-detection: %s: %s", ip, entry)
		} else {
			log.Debugf("portscan-detection: Removed %s from the list", ip)
		}
	}
	log.Debugf("portscan-detection: finished update list&cleanup")

}

func start() error {
	ips = make(map[string]*ipData)

	// cleanup old Threats
	threats, _ := status.GetThreats(threatIDPrefix)
	for _, t := range threats {
		status.DeleteThreat(t.ID)
	}

	log.Debugf("portscan-detection: starting")
	err := inspection.RegisterInspector(&inspection.Registration{
		Name:    "Portscan Detection",
		Order:   0,
		Factory: DetectorFactory,
	})

	if err != nil {
		return err
	}

	module.NewTask("portscan score update", func(ctx context.Context, task *modules.Task) error {
		updateWholeList()
		return nil
	}).Repeat(cleanUpInterval).MaxDelay(cleanUpMaxDelay)

	return nil
}

func isNetBIOSoverTCPIP(conn *network.Connection) bool {
	return conn.LocalPort == 137 || // maybe we could limit this to UDP ... RFC1002 defines NAME_SERVICE_TCP_PORT but dosn't use it (in contrast to the other ports that are also only defined TCP or UDP)
		(conn.IPProtocol == packet.UDP && conn.LocalPort == 138) ||
		(conn.IPProtocol == packet.TCP && conn.LocalPort == 139)

}

func intMin(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (ip *ipData) String() string {
	var blockedPorts strings.Builder
	for k, v := range ip.blockedPorts {
		if k > 0 {
			blockedPorts.WriteString(", ")
		}

		blockedPorts.WriteString(v.protocol.String() + " " + strconv.Itoa(int(v.port)))
	}

	return fmt.Sprintf("Score: %d, previousOffender: %t, blocked: %t, ignored: %t, lastSeen: %s, lastUpdated: %s, blockedPorts: [%s]", ip.score, ip.previousOffender, ip.blocked, ip.ignore, ip.lastSeen, ip.lastUpdated, blockedPorts.String())
}

func extractRemoteFromIPString(ipString string) string {
	return strings.SplitAfterN(ipString, "-", 2)[1]
}
