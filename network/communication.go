// Copyright Safing ICS Technologies GmbH. Use of this source code is governed by the AGPL license that can be found in the LICENSE file.

package network

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/Safing/portbase/database/record"
	"github.com/Safing/portbase/log"
	"github.com/Safing/portmaster/intel"
	"github.com/Safing/portmaster/network/netutils"
	"github.com/Safing/portmaster/network/packet"
	"github.com/Safing/portmaster/process"
	"github.com/Safing/portmaster/profile"
)

// Communication describes a logical connection between a process and a domain.
type Communication struct {
	record.Base
	sync.Mutex

	Domain    string
	Direction bool
	Intel     *intel.Intel
	process   *process.Process
	Verdict   Verdict
	Reason    string
	Inspect   bool

	FirstLinkEstablished int64
	LastLinkEstablished  int64

	profileUpdateVersion uint32
	saveWhenFinished     bool
}

// Process returns the process that owns the connection.
func (comm *Communication) Process() *process.Process {
	comm.Lock()
	defer comm.Unlock()

	return comm.process
}

// ResetVerdict resets the verdict to VerdictUndecided.
func (comm *Communication) ResetVerdict() {
	comm.Lock()
	defer comm.Unlock()

	comm.Verdict = VerdictUndecided
	comm.Reason = ""
	comm.saveWhenFinished = true
}

// GetVerdict returns the current verdict.
func (comm *Communication) GetVerdict() Verdict {
	comm.Lock()
	defer comm.Unlock()

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
	comm.Lock()
	defer comm.Unlock()

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

	comm.Lock()
	defer comm.Unlock()
	comm.Reason = reason
	comm.saveWhenFinished = true
}

// AddReason adds a human readable string as to why a certain verdict was set in regard to this communication.
func (comm *Communication) AddReason(reason string) {
	if reason == "" {
		return
	}

	comm.Lock()
	defer comm.Unlock()

	if comm.Reason != "" {
		comm.Reason += " | "
	}
	comm.Reason += reason
}

// NeedsReevaluation returns whether the decision on this communication should be re-evaluated.
func (comm *Communication) NeedsReevaluation() bool {
	comm.Lock()
	defer comm.Unlock()

	oldVersion := comm.profileUpdateVersion
	comm.profileUpdateVersion = profile.GetUpdateVersion()

	if oldVersion == 0 {
		return false
	}
	if oldVersion != comm.profileUpdateVersion {
		return true
	}
	return false
}

// GetCommunicationByFirstPacket returns the matching communication from the internal storage.
func GetCommunicationByFirstPacket(pkt packet.Packet) (*Communication, error) {
	// get Process
	proc, direction, err := process.GetProcessByPacket(pkt)
	if err != nil {
		return nil, err
	}
	var domain string

	// Incoming
	if direction {
		switch netutils.ClassifyIP(pkt.Info().Src) {
		case netutils.HostLocal:
			domain = IncomingHost
		case netutils.LinkLocal, netutils.SiteLocal, netutils.LocalMulticast:
			domain = IncomingLAN
		case netutils.Global, netutils.GlobalMulticast:
			domain = IncomingInternet
		case netutils.Invalid:
			domain = IncomingInvalid
		}

		communication, ok := GetCommunication(proc.Pid, domain)
		if !ok {
			communication = &Communication{
				Domain:               domain,
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
	ipinfo, err := intel.GetIPInfo(pkt.FmtRemoteIP())

	// PeerToPeer
	if err != nil {
		// if no domain could be found, it must be a direct connection (ie. no DNS)

		switch netutils.ClassifyIP(pkt.Info().Dst) {
		case netutils.HostLocal:
			domain = PeerHost
		case netutils.LinkLocal, netutils.SiteLocal, netutils.LocalMulticast:
			domain = PeerLAN
		case netutils.Global, netutils.GlobalMulticast:
			domain = PeerInternet
		case netutils.Invalid:
			domain = PeerInvalid
		}

		communication, ok := GetCommunication(proc.Pid, domain)
		if !ok {
			communication = &Communication{
				Domain:               domain,
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
			Domain:               ipinfo.Domains[0],
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
			Domain:           fqdn,
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
	return fmt.Sprintf("%d/%s", comm.process.Pid, comm.Domain)
}

// SaveWhenFinished marks the Connection for saving after all current actions are finished.
func (comm *Communication) SaveWhenFinished() {
	comm.saveWhenFinished = true
}

// SaveIfNeeded saves the Connection if it is marked for saving when finished.
func (comm *Communication) SaveIfNeeded() {
	comm.Lock()
	save := comm.saveWhenFinished
	if save {
		comm.saveWhenFinished = false
	}
	comm.Unlock()

	if save {
		comm.save()
	}
}

// Save saves the Connection object in the storage and propagates the change.
func (comm *Communication) save() error {
	// update comm
	comm.Lock()
	if comm.process == nil {
		comm.Unlock()
		return errors.New("cannot save connection without process")
	}

	if !comm.KeyIsSet() {
		comm.SetKey(fmt.Sprintf("network:tree/%d/%s", comm.process.Pid, comm.Domain))
		comm.UpdateMeta()
	}
	if comm.Meta().Deleted > 0 {
		log.Criticalf("network: revieving dead comm %s", comm)
		comm.Meta().Deleted = 0
	}
	key := comm.makeKey()
	comm.saveWhenFinished = false
	comm.Unlock()

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
	comm.Lock()
	defer comm.Unlock()

	delete(comms, comm.makeKey())

	comm.Meta().Delete()
	go dbController.PushUpdate(comm)
}

// AddLink applies the Communication to the Link and sets timestamps.
func (comm *Communication) AddLink(link *Link) {
	// apply comm to link
	link.Lock()
	link.comm = comm
	link.Verdict = comm.Verdict
	link.Inspect = comm.Inspect
	link.saveWhenFinished = true
	link.Unlock()

	// update comm LastLinkEstablished
	comm.Lock()

	// check if we should save
	if comm.LastLinkEstablished < time.Now().Add(-3*time.Second).Unix() {
		comm.saveWhenFinished = true
	}

	// update LastLinkEstablished
	comm.LastLinkEstablished = time.Now().Unix()
	if comm.FirstLinkEstablished == 0 {
		comm.FirstLinkEstablished = comm.LastLinkEstablished
	}

	comm.Unlock()
}

// String returns a string representation of Communication.
func (comm *Communication) String() string {
	comm.Lock()
	defer comm.Unlock()

	switch comm.Domain {
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
			return fmt.Sprintf("? -> %s", comm.Domain)
		}
		return fmt.Sprintf("%s -> %s", comm.process.String(), comm.Domain)
	}
}
