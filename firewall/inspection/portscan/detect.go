package portscan

import (
	"context"
	"net"
	"sync"
	"time"

	"github.com/safing/portbase/log"
	"github.com/safing/portbase/modules"
	"github.com/safing/portmaster/firewall/inspection"
	"github.com/safing/portmaster/netenv"
	"github.com/safing/portmaster/network"
	"github.com/safing/portmaster/network/netutils"
	"github.com/safing/portmaster/network/packet"
	"github.com/safing/portmaster/process"
	"github.com/safing/portmaster/status"
)

type tcpUDPport struct {
	protocol packet.IPProtocol
	port     uint16
}

type ipData struct {
	score int //score needs to be big enough to keep maxScore + addScore... to prevent overflow
	//	greylistingWorked bool
	previousOffender bool
	blocked          bool
	ignore           bool
	lastSeen         time.Time
	lastUpdated      time.Time
	blockedPorts     []tcpUDPport
}

const (
	//fixme
	cleanUpInterval = 1 * time.Minute
	cleanUpMaxDelay = 5 * time.Minute

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
	ips    map[string]*ipData
	ownIPs []net.IP

	module     *modules.Module
	runOnlyOne sync.Mutex
)

// Detector detects if a connection is encrypted.
type Detector struct{}

// Name implements the inspection interface.
func (d *Detector) Name() string {
	return "Portscan Detection"
}

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

	log.Tracer(ctx).Debugf("Conn: %v, Entity: %#v, Protocol: %v, LocalIP: %s, LocalPort: %d, inMap: %v, entry: %+v", conn, conn.Entity, conn.IPProtocol, conn.LocalIP.String(), conn.LocalPort, inMap, entry)

	if inMap {
		entry.updateScoreIgnoreBlockPrevOffender(ipString)
		entry.lastSeen = time.Now()

		if entry.ignore {
			return network.VerdictUndecided, false, nil
		}
	}

	ipClass := netutils.ClassifyIP(conn.LocalIP)
	proc := conn.Process()

	log.Tracer(ctx).Debugf("PID: %+v", proc)

	//malicious Packet?
	if (proc == nil || proc.Pid == process.UnidentifiedProcessID) && //Port unused
		conn.Inbound &&
		(conn.IPProtocol == packet.TCP || conn.IPProtocol == packet.UDP) &&
		!foreignIPv4(conn.LocalIP) &&
		(ipClass == netutils.LinkLocal ||
			ipClass == netutils.SiteLocal ||
			ipClass == netutils.Invalid ||
			ipClass == netutils.Global) &&
		!isNetBIOSoverTCPIP(conn) &&
		!(conn.IPProtocol == packet.UDP && (conn.LocalPort == 67 || conn.LocalPort == 68)) { // DHCP

		handleMaliciousPacket(ctx, inMap, conn, entry, ipString)
	}

	if inMap && entry.blocked {
		log.Tracer(ctx).Debugf("blocking")
		conn.SetVerdict(network.VerdictDrop, "Portscan", nil)
	} else {
		log.Tracer(ctx).Debugf("let through")
	}

	return network.VerdictUndecided, false, nil
}

func handleMaliciousPacket(ctx context.Context, inMap bool, conn *network.Connection, entry *ipData, ipString string) {
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
			blockedPorts: []tcpUDPport{
				tcpUDPport{protocol: conn.IPProtocol, port: conn.LocalPort},
			},
			lastSeen:    time.Now(),
			lastUpdated: time.Now(),
		}
		log.Tracer(ctx).Debugf("New Entry: %+v", ips[ipString])
	} else {
		//Port in list of tried ports?
		triedPort := false
		port := tcpUDPport{protocol: conn.IPProtocol, port: conn.LocalPort}
		for _, e := range entry.blockedPorts {
			if e == port {
				triedPort = true
				break
			}
		}

		if !triedPort {
			entry.blockedPorts = append(entry.blockedPorts, tcpUDPport{protocol: conn.IPProtocol, port: conn.LocalPort})
			entry.score = intMin(entry.score+addScore, maxScore)

			if entry.previousOffender || entry.score >= scoreBlock {
				entry.blocked = true
				entry.previousOffender = true

				//fixme: actually I just want to know if THIS threat exists - I don't need prefixing. Maybe we can do it simpler ...
				if t, _ := status.GetThreats(threadPrefix + ipString); len(t) == 0 {
					log.Tracer(ctx).Debugf("new Threat")
					status.AddOrUpdateThreat(&status.Threat{
						ID:              threadPrefix + ipString,
						Name:            "Portscan by " + ipString,
						Description:     "Someone tries to connect to a lot of closed Ports (non-running Services). Probably he wants to find out the services running on the maschine to determine which services to attack", //fixme: to long
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
	d.score -= intMin(int(time.Since(d.lastUpdated)/decreaseInterval), d.score)

	if d.ignore {
		if time.Since(d.lastSeen) > unignoreTime {
			d.ignore = false
		}
	}

	if d.previousOffender && time.Since(d.lastSeen) > undoSuspicionIdleTime {
		d.previousOffender = false
	}

	if d.blocked && time.Since(d.lastSeen) > unblockIdleTime {
		d.blocked = false
		d.blockedPorts = []tcpUDPport{}

		status.DeleteThreat(threadPrefix + ipString)
	}

	d.lastUpdated = time.Now()
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

	//cleanup old Threads
	threads, _ := status.GetThreats(threadPrefix)
	for _, t := range threads {
		status.DeleteThreat(t.ID)
	}

	module = modules.Register("portscan-detection", nil, start, nil, "base", "netenv")
	module.Enable()
}

func updateWholeList() {
	log.Debugf("Portscan detection: update list&cleanup")
	for ip, entry := range ips {
		//done inside the loop to give other goroutines time in between to access the list (and during that time block this task)
		runOnlyOne.Lock()
		defer runOnlyOne.Unlock()

		entry.updateScoreIgnoreBlockPrevOffender(ip)
		log.Debugf("%s: %v", ip, entry)
	}
	log.Debugf("Portscan detection: finished update list&cleanup")

}

func start() (err error) {
	go delayedStart()

	// Reload own IP List on Network change
	err = module.RegisterEventHook(
		"netenv",
		"network changed",
		"Reload List of own IPs on Network change for Portscan detection",
		func(_ context.Context, _ interface{}) (err error) {
			fillOwnIPs()

			return
		},
	)

	fillOwnIPs()

	return
}

func delayedStart() {
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

	module.NewTask("portscan score update", func(ctx context.Context, task *modules.Task) (err error) {
		updateWholeList()
		return
	}).Repeat(cleanUpInterval).MaxDelay(cleanUpMaxDelay)
}

func fillOwnIPs() {
	var err error
	ownIPs, _, err = netenv.GetAssignedAddresses()

	if err != nil {
		log.Errorf("Couldn't obtain List of IPs: %v", err)
	}

	log.Debugf("Portscan detection: ownIPs: %v", ownIPs)
}

//Does NOT check localhost range!!
func foreignIPv4(ip net.IP) bool {
	if ip.To4() == nil {
		return false
	}

	for _, ownIP := range ownIPs {
		if ip.Equal(ownIP) {
			return false
		}
	}

	return true
}

func isNetBIOSoverTCPIP(conn *network.Connection) bool {
	return conn.LocalPort == 138 ||
		(conn.IPProtocol == packet.UDP && conn.LocalPort == 138) ||
		(conn.IPProtocol == packet.TCP && conn.LocalPort == 139)

}

// Source: https://stackoverflow.com/questions/27516387/what-is-the-correct-way-to-find-the-min-between-two-integers-in-go#27516559
func intMin(a, b int) int {
	if a < b {
		return a
	}
	return b
}
