package portscan

import (
	"context"
	"net"
	"sync"
	"time"

	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/firewall/inspection"
	"github.com/safing/portmaster/network"
	"github.com/safing/portmaster/network/netutils"
	"github.com/safing/portmaster/network/packet"
	"github.com/safing/portmaster/process"
	"github.com/safing/portmaster/status"
)

//TODO: Integer over-&Underflow on score

type ipData struct {
	score int //score needs to be big enough to keep maxScore + addScore... to prevent overflow
	//	greylistingWorked bool
	previousOffender bool
	blocked          bool
	ignore           bool
	lastSeen         time.Time
	blockedPorts     []uint16
}

const (
	//fixme
	startAfter            = 1 * time.Second //When should the Portscan Detection start to prevent blocking Apps that just try to reconnect?
	decreaseInterval      = 11 * time.Second
	unblockIdleTime       = 1 * time.Hour
	undoSuspicionIdleTime = 24 * time.Hour
	unignoreTime          = 24 * time.Hour

	startRegisteredPorts = 1024
	startDynamicPorts    = 32768

	addScoreWellKnownPort  = 40
	addScoreRegisteredPort = 20
	addScoreDynamicPort    = 10

	scoreBlock = 160
	maxScore   = 320

	threadPrefix = "portscan: "
)

var (
	ips map[string]*ipData

	runOnlyOne sync.Mutex
)

// Detector detects if a connection is encrypted.
type Detector struct{}

// Name implements the inspection interface.
func (d *Detector) Name() string {
	return "Portscan Detection"
}

//TODO: regular cleanup (to shrink the size down & stop warnings)
//TODO: handle TCP UDP separately
//TODO: ignore own IPs as source

// Inspect implements the inspection interface.
func (d *Detector) Inspect(conn *network.Connection, pkt packet.Packet) (pktVerdict network.Verdict, proceed bool, err error) {
	runOnlyOne.Lock()
	defer runOnlyOne.Unlock()

	ctx := pkt.Ctx()

	//fixme: DEL
	if conn.LocalIP.Equal(net.IP([]byte{255, 255, 255, 255})) {
		return network.VerdictUndecided, false, nil
	}
	log.Tracer(ctx).Debugf("new connection for Portscan detection")

	rIP, ok := conn.Entity.GetIP() //remote IP
	if !ok {                       //No IP => return undecided
		return network.VerdictUndecided, false, nil
	}

	ipString := rIP.String()
	entry, inMap := ips[ipString]

	log.Tracer(ctx).Debugf("Conn: %#v, Entity: %#v, Protocol: %v, LocalIP: %s, LocalPort: %d, inMap: %v, entry: %+v", conn, conn.Entity, conn.IPProtocol, conn.LocalIP.String(), conn.LocalPort, inMap, entry)

	if inMap {
		entry.updateScoreIgnoreBlockPrevOffender(ipString)

		if entry.ignore {
			return network.VerdictUndecided, false, nil
		}
	}

	ipClass := netutils.ClassifyIP(conn.LocalIP)
	proc := conn.Process()

	log.Tracer(ctx).Debugf("PID: %+v", proc)

	//malicious Packet?
	if proc != nil && proc.Pid == process.UnidentifiedProcessID && //Port unused
		conn.Inbound &&
		(conn.IPProtocol == packet.TCP || conn.IPProtocol == packet.UDP) &&
		!pseudoIsBCast(conn.LocalIP) && //not sent to a Broadast-Address
		(ipClass == netutils.LinkLocal ||
			ipClass == netutils.SiteLocal ||
			ipClass == netutils.Invalid ||
			ipClass == netutils.Global) &&
		!isNetBIOSoverTCPIP(conn) &&
		!(conn.IPProtocol == packet.UDP && (conn.LocalPort == 67 || conn.LocalPort == 68)) { // DHCP

		handleMaliciousPacket(inMap, conn, entry, ipString, ctx)
	}

	if inMap && entry.blocked {
		log.Tracer(ctx).Debugf("blocking")
		conn.SetVerdict(network.VerdictDrop, "Portscan", nil)
	} else {
		log.Tracer(ctx).Debugf("let through")
	}

	return network.VerdictUndecided, false, nil
}

func handleMaliciousPacket(inMap bool, conn *network.Connection, entry *ipData, ipString string, ctx context.Context) {
	//define Portscore
	var addScore int
	switch {
	case conn.LocalPort < startRegisteredPorts:
		addScore = addScoreWellKnownPort
	case conn.LocalPort < startDynamicPorts:
		addScore = addScoreRegisteredPort
	default:
		addScore = addScoreDynamicPort
	}

	if !inMap {
		//new IP => add to List
		ips[ipString] = &ipData{
			score: addScore,
			blockedPorts: []uint16{
				conn.LocalPort,
			},
			lastSeen: time.Now(),
		}
		log.Tracer(ctx).Debugf("New Entry: %+v", ips[ipString])
	} else {
		//Port in list of tried ports?
		triedPort := false
		for _, e := range entry.blockedPorts {
			if conn.LocalPort == e {
				triedPort = true
			}
		}

		if !triedPort {
			entry.blockedPorts = append(entry.blockedPorts, conn.LocalPort)
			entry.score = intMin(entry.score+addScore, maxScore)

			if entry.previousOffender || entry.score >= scoreBlock {
				entry.blocked = true
				entry.previousOffender = true

				//TODO: actually I just want to know if THIS threat exists - I don't need prefixing. Maybe we can do it simpler ...
				if t, _ := status.GetThreats(threadPrefix + ipString); len(t) == 0 {
					log.Tracer(ctx).Debugf("new Threat")
					status.AddOrUpdateThreat(&status.Threat{
						ID:              threadPrefix + ipString,
						Name:            "Portscan by " + ipString,
						Description:     "The Computer with the address " + ipString + " tries to connect to a lot of closed Ports (non-running Applications). Probably he wants to find out the services running on the maschine to determine which services to attack",
						MitigationLevel: status.SecurityLevelHigh,
						Started:         time.Now().Unix(),
					})
				}
			}
		}

		log.Tracer(ctx).Debugf("changed Entry: %+v", entry)
	}
}

//updateScoreIgnoreBlockPrevOffender updates this 4 Values of the Struct
//ipString needs to correspond to the key of the entry in the map ips
func (d *ipData) updateScoreIgnoreBlockPrevOffender(ipString string) {
	oldLastSeen := d.lastSeen
	d.lastSeen = time.Now()

	d.score -= intMin(int(d.lastSeen.Sub(oldLastSeen)/decreaseInterval), d.score)

	if d.ignore {
		if d.lastSeen.Sub(oldLastSeen) > unignoreTime {
			d.ignore = false
		}
	}

	if d.previousOffender && d.lastSeen.Sub(oldLastSeen) > undoSuspicionIdleTime {
		d.previousOffender = false
	}

	if d.blocked && d.lastSeen.Sub(oldLastSeen) > unblockIdleTime {
		d.blocked = false
		d.blockedPorts = []uint16{}

		status.DeleteThreat(threadPrefix + ipString)
	}
}

//TODO: make real check since the current version can be abused by an attacker if the SNM is < /24; check both local and global bcast
//TODO: use netenv.GetAssignedAddresses()
func pseudoIsBCast(ip net.IP) bool {
	ip4 := ip.To4()
	return ip4 != nil && ip4[3] == 255 //gladly IPv6 doesn't have Broadcasts anymore
}

func isNetBIOSoverTCPIP(conn *network.Connection) bool {
	return conn.LocalPort == 138 ||
		(conn.IPProtocol == packet.UDP && conn.LocalPort == 138) ||
		(conn.IPProtocol == packet.TCP && conn.LocalPort == 139)

}

// Destroy implements the destroy interface.
func (d *Detector) Destroy() error {
	return nil
}

// DetectorFactory is a primitive detection method that runs within the factory only.
func DetectorFactory(conn *network.Connection, pkt packet.Packet) (network.Inspector, error) {
	return &Detector{}, nil
}

// Register registers the encryption detection inspector with the inspection framework.
func init() {
	ips = make(map[string]*ipData)

	threads, _ := status.GetThreats(threadPrefix)
	for _, t := range threads {
		status.DeleteThreat(t.ID)
	}

	go start()
}

func start() {
	//TODO: Any Problems killing main gorouting during this time?
	time.Sleep(startAfter)

	log.Debugf("starting Portscan detection")
	err := inspection.RegisterInspector(&inspection.Registration{
		Name:    "Portscan Detection",
		Order:   0,
		Factory: DetectorFactory,
	})
	if err != nil {
		panic(err)
	}
	log.Debugf("started Portscan detection")
}

// Source: https://stackoverflow.com/questions/27516387/what-is-the-correct-way-to-find-the-min-between-two-integers-in-go#27516559
func intMin(a, b int) int {
	if a < b {
		return a
	}
	return b
}
