package network

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/safing/portmaster/resolver"

	"github.com/safing/portbase/database/record"
	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/intel"
	"github.com/safing/portmaster/network/netutils"
	"github.com/safing/portmaster/network/packet"
	"github.com/safing/portmaster/process"
)

// Communication describes a logical connection between a process and a domain.
//nolint:maligned // TODO: fix alignment
type Communication struct {
	record.Base
	lock sync.Mutex

	Scope     string
	Entity    *intel.Entity
	Direction bool

	Verdict                Verdict
	Reason                 string
	ReasonID               string // format source[:id[:id]]
	Inspect                bool
	process                *process.Process
	profileRevisionCounter uint64

	FirstLinkEstablished int64
	LastLinkEstablished  int64

	saveWhenFinished bool
}

// Lock locks the communication and the communication's Entity.
func (comm *Communication) Lock() {
	comm.lock.Lock()
	comm.Entity.Lock()
}

// Lock unlocks the communication and the communication's Entity.
func (comm *Communication) Unlock() {
	comm.Entity.Unlock()
	comm.lock.Unlock()
}

// Process returns the process that owns the connection.
func (comm *Communication) Process() *process.Process {
	comm.lock.Lock()
	defer comm.lock.Unlock()

	return comm.process
}

// ResetVerdict resets the verdict to VerdictUndecided.
func (comm *Communication) ResetVerdict() {
	comm.lock.Lock()
	defer comm.lock.Unlock()

	comm.Verdict = VerdictUndecided
	comm.Reason = ""
	comm.saveWhenFinished = true
}

// GetVerdict returns the current verdict.
func (comm *Communication) GetVerdict() Verdict {
	comm.lock.Lock()
	defer comm.lock.Unlock()

	return comm.Verdict
}

// Accept accepts the communication and adds the given reason.
func (comm *Communication) Accept(reason string) {
	comm.AddReason(reason)
	comm.UpdateVerdict(VerdictAccept)
}

// Deny blocks or drops the communication depending on the connection direction and adds the given reason.
func (comm *Communication) Deny(reason string) {
	if comm.Direction {
		comm.Drop(reason)
	} else {
		comm.Block(reason)
	}
}

// Block blocks the communication and adds the given reason.
func (comm *Communication) Block(reason string) {
	comm.AddReason(reason)
	comm.UpdateVerdict(VerdictBlock)
}

// Drop drops the communication and adds the given reason.
func (comm *Communication) Drop(reason string) {
	comm.AddReason(reason)
	comm.UpdateVerdict(VerdictDrop)
}

// UpdateVerdict sets a new verdict for this link, making sure it does not interfere with previous verdicts.
func (comm *Communication) UpdateVerdict(newVerdict Verdict) {
	comm.lock.Lock()
	defer comm.lock.Unlock()

	if newVerdict > comm.Verdict {
		comm.Verdict = newVerdict
		comm.saveWhenFinished = true
	}
}

// SetReason sets/replaces a human readable string as to why a certain verdict was set in regard to this communication.
func (comm *Communication) SetReason(reason string) {
	if reason == "" {
		return
	}

	comm.lock.Lock()
	defer comm.lock.Unlock()
	comm.Reason = reason
	comm.saveWhenFinished = true
}

// AddReason adds a human readable string as to why a certain verdict was set in regard to this communication.
func (comm *Communication) AddReason(reason string) {
	if reason == "" {
		return
	}

	comm.lock.Lock()
	defer comm.lock.Unlock()

	if comm.Reason != "" {
		comm.Reason += " | "
	}
	comm.Reason += reason
}

// UpdateAndCheck updates profiles and checks whether a reevaluation is needed.
func (comm *Communication) UpdateAndCheck() (needsReevaluation bool) {
	revCnt := comm.Process().Profile().Update()

	comm.lock.Lock()
	defer comm.lock.Unlock()
	if comm.profileRevisionCounter != revCnt {
		comm.profileRevisionCounter = revCnt
		needsReevaluation = true
	}

	return
}

// GetCommunicationByFirstPacket returns the matching communication from the internal storage.
func GetCommunicationByFirstPacket(pkt packet.Packet) (*Communication, error) {
	// get Process
	proc, direction, err := process.GetProcessByPacket(pkt)
	if err != nil {
		return nil, err
	}
	var scope string

	// Incoming
	if direction {
		switch netutils.ClassifyIP(pkt.Info().Src) {
		case netutils.HostLocal:
			scope = IncomingHost
		case netutils.LinkLocal, netutils.SiteLocal, netutils.LocalMulticast:
			scope = IncomingLAN
		case netutils.Global, netutils.GlobalMulticast:
			scope = IncomingInternet
		case netutils.Invalid:
			scope = IncomingInvalid
		}

		communication, ok := GetCommunication(proc.Pid, scope)
		if !ok {
			communication = &Communication{
				Scope:                scope,
				Entity:               (&intel.Entity{}).Init(),
				Direction:            Inbound,
				process:              proc,
				Inspect:              true,
				FirstLinkEstablished: time.Now().Unix(),
				saveWhenFinished:     true,
			}
		}
		communication.process.AddCommunication()
		return communication, nil
	}

	// get domain
	ipinfo, err := resolver.GetIPInfo(pkt.FmtRemoteIP())

	// PeerToPeer
	if err != nil {
		// if no domain could be found, it must be a direct connection (ie. no DNS)

		switch netutils.ClassifyIP(pkt.Info().Dst) {
		case netutils.HostLocal:
			scope = PeerHost
		case netutils.LinkLocal, netutils.SiteLocal, netutils.LocalMulticast:
			scope = PeerLAN
		case netutils.Global, netutils.GlobalMulticast:
			scope = PeerInternet
		case netutils.Invalid:
			scope = PeerInvalid
		}

		communication, ok := GetCommunication(proc.Pid, scope)
		if !ok {
			communication = &Communication{
				Scope:                scope,
				Entity:               (&intel.Entity{}).Init(),
				Direction:            Outbound,
				process:              proc,
				Inspect:              true,
				FirstLinkEstablished: time.Now().Unix(),
				saveWhenFinished:     true,
			}
		}
		communication.process.AddCommunication()
		return communication, nil
	}

	// To Domain
	// FIXME: how to handle multiple possible domains?
	communication, ok := GetCommunication(proc.Pid, ipinfo.Domains[0])
	if !ok {
		communication = &Communication{
			Scope: ipinfo.Domains[0],
			Entity: (&intel.Entity{
				Domain: ipinfo.Domains[0],
			}).Init(),
			Direction:            Outbound,
			process:              proc,
			Inspect:              true,
			FirstLinkEstablished: time.Now().Unix(),
			saveWhenFinished:     true,
		}
	}
	communication.process.AddCommunication()
	return communication, nil
}

// var localhost = net.IPv4(127, 0, 0, 1)

var (
	dnsAddress        = net.IPv4(127, 0, 0, 1)
	dnsPort    uint16 = 53
)

// GetCommunicationByDNSRequest returns the matching communication from the internal storage.
func GetCommunicationByDNSRequest(ctx context.Context, ip net.IP, port uint16, fqdn string) (*Communication, error) {
	// get Process
	proc, err := process.GetProcessByEndpoints(ctx, ip, port, dnsAddress, dnsPort, packet.UDP)
	if err != nil {
		return nil, err
	}

	communication, ok := GetCommunication(proc.Pid, fqdn)
	if !ok {
		communication = &Communication{
			Scope: fqdn,
			Entity: (&intel.Entity{
				Domain: fqdn,
			}).Init(),
			process:          proc,
			Inspect:          true,
			saveWhenFinished: true,
		}
		communication.process.AddCommunication()
		communication.saveWhenFinished = true
	}
	return communication, nil
}

// GetCommunication fetches a connection object from the internal storage.
func GetCommunication(pid int, domain string) (comm *Communication, ok bool) {
	commsLock.RLock()
	defer commsLock.RUnlock()
	comm, ok = comms[fmt.Sprintf("%d/%s", pid, domain)]
	return
}

func (comm *Communication) makeKey() string {
	return fmt.Sprintf("%d/%s", comm.process.Pid, comm.Scope)
}

// SaveWhenFinished marks the Connection for saving after all current actions are finished.
func (comm *Communication) SaveWhenFinished() {
	comm.saveWhenFinished = true
}

// SaveIfNeeded saves the Connection if it is marked for saving when finished.
func (comm *Communication) SaveIfNeeded() {
	comm.lock.Lock()
	save := comm.saveWhenFinished
	if save {
		comm.saveWhenFinished = false
	}
	comm.lock.Unlock()

	if save {
		err := comm.save()
		if err != nil {
			log.Warningf("network: failed to save comm %s: %s", comm, err)
		}
	}
}

// Save saves the Connection object in the storage and propagates the change.
func (comm *Communication) save() error {
	// update comm
	comm.lock.Lock()
	if comm.process == nil {
		comm.lock.Unlock()
		return errors.New("cannot save connection without process")
	}

	if !comm.KeyIsSet() {
		comm.SetKey(fmt.Sprintf("network:tree/%d/%s", comm.process.Pid, comm.Scope))
		comm.UpdateMeta()
	}
	if comm.Meta().Deleted > 0 {
		log.Criticalf("network: revieving dead comm %s", comm)
		comm.Meta().Deleted = 0
	}
	key := comm.makeKey()
	comm.saveWhenFinished = false
	comm.lock.Unlock()

	// save comm
	commsLock.RLock()
	_, ok := comms[key]
	commsLock.RUnlock()

	if !ok {
		commsLock.Lock()
		comms[key] = comm
		commsLock.Unlock()
	}

	go dbController.PushUpdate(comm)
	return nil
}

// Delete deletes a connection from the storage and propagates the change.
func (comm *Communication) Delete() {
	commsLock.Lock()
	defer commsLock.Unlock()
	comm.lock.Lock()
	defer comm.lock.Unlock()

	delete(comms, comm.makeKey())

	comm.Meta().Delete()
	go dbController.PushUpdate(comm)
}

// AddLink applies the Communication to the Link and sets timestamps.
func (comm *Communication) AddLink(link *Link) {
	comm.lock.Lock()
	defer comm.lock.Unlock()

	// apply comm to link
	link.lock.Lock()
	link.comm = comm
	link.Verdict = comm.Verdict
	link.Inspect = comm.Inspect
	// FIXME: use new copy methods
	link.Entity.Domain = comm.Entity.Domain
	link.saveWhenFinished = true
	link.lock.Unlock()

	// check if we should save
	if comm.LastLinkEstablished < time.Now().Add(-3*time.Second).Unix() {
		comm.saveWhenFinished = true
	}

	// update LastLinkEstablished
	comm.LastLinkEstablished = time.Now().Unix()
	if comm.FirstLinkEstablished == 0 {
		comm.FirstLinkEstablished = comm.LastLinkEstablished
	}
}

// String returns a string representation of Communication.
func (comm *Communication) String() string {
	comm.Lock()
	defer comm.Unlock()

	switch comm.Scope {
	case IncomingHost, IncomingLAN, IncomingInternet, IncomingInvalid:
		if comm.process == nil {
			return "? <- *"
		}
		return fmt.Sprintf("%s <- *", comm.process.String())
	case PeerHost, PeerLAN, PeerInternet, PeerInvalid:
		if comm.process == nil {
			return "? -> *"
		}
		return fmt.Sprintf("%s -> *", comm.process.String())
	default:
		if comm.process == nil {
			return fmt.Sprintf("? -> %s", comm.Scope)
		}
		return fmt.Sprintf("%s -> %s", comm.process.String(), comm.Scope)
	}
}
