package network

// Verdict describes the decision made about a connection or link.
type Verdict int8

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

func (v Verdict) String() string {
	switch v {
	case VerdictUndecided:
		return "<Undecided>"
	case VerdictUndeterminable:
		return "<Undeterminable>"
	case VerdictAccept:
		return "Accept"
	case VerdictBlock:
		return "Block"
	case VerdictDrop:
		return "Drop"
	case VerdictRerouteToNameserver:
		return "RerouteToNameserver"
	case VerdictRerouteToTunnel:
		return "RerouteToTunnel"
	default:
		return "<INVALID VERDICT>"
	}
}

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
