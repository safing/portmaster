package network

import "github.com/Safing/portmaster/process"

// Static reasons
const (
	ReasonUnknownProcess = "unknown connection owner: process could not be found"
)

var (
	UnknownDirectConnection = &Connection{
		Domain:    "PI",
		Direction: Outbound,
		Verdict:   DROP,
		Reason:    ReasonUnknownProcess,
		process:   process.UnknownProcess,
	}

	UnknownIncomingConnection = &Connection{
		Domain:    "II",
		Direction: Inbound,
		Verdict:   DROP,
		Reason:    ReasonUnknownProcess,
		process:   process.UnknownProcess,
	}
)

func init() {
	UnknownDirectConnection.Save()
	UnknownIncomingConnection.Save()
}
