package packet

import "fmt"

// BandwidthUpdate holds an update to the seen bandwidth of a connection.
type BandwidthUpdate struct {
	ConnID    string
	RecvBytes uint64
	SentBytes uint64
	Method    BandwidthUpdateMethod
}

// BandwidthUpdateMethod defines how the bandwidth data of a bandwidth update should be interpreted.
type BandwidthUpdateMethod uint8

// Bandwidth Update Methods.
const (
	Absolute BandwidthUpdateMethod = iota
	Additive
)

func (bu *BandwidthUpdate) String() string {
	return fmt.Sprintf("%s: %dB recv | %dB sent [%s]", bu.ConnID, bu.RecvBytes, bu.SentBytes, bu.Method)
}

func (bum BandwidthUpdateMethod) String() string {
	switch bum {
	case Absolute:
		return "absolute"
	case Additive:
		return "additive"
	default:
		return "unknown"
	}
}
