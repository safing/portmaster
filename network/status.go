// Copyright Safing ICS Technologies GmbH. Use of this source code is governed by the AGPL license that can be found in the LICENSE file.

package network

// Verdict describes the decision made about a connection or link.
type Verdict uint8

// List of values a Status can have
const (
	// UNDECIDED is the default status of new connections
	VerdictUndecided           Verdict = 0
	VerdictUndeterminable      Verdict = 1
	VerdictAccept              Verdict = 2
	VerdictBlock               Verdict = 3
	VerdictDrop                Verdict = 4
	VerdictRerouteToNameserver Verdict = 5
	VerdictRerouteToTunnel     Verdict = 6
)

// Packer Directions
const (
	Inbound  = true
	Outbound = false
)

// Non-Domain Connections
const (
	IncomingHost     = "IH"
	IncomingLAN      = "IL"
	IncomingInternet = "II"
	IncomingInvalid  = "IX"
	PeerHost         = "PH"
	PeerLAN          = "PL"
	PeerInternet     = "PI"
	PeerInvalid      = "PX"
)
