package profile

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Safing/portmaster/network/reference"
)

// Ports is a list of permitted or denied ports
type Ports map[int16][]*Port

// Check returns whether listening/connecting to a certain port is allowed, if set.
func (p Ports) Check(signedProtocol int16, port uint16) (permit, ok bool) {
	if p == nil {
		return false, false
	}

	portDefinitions, ok := p[signedProtocol]
	if ok {
		for _, portD := range portDefinitions {
			if portD.Matches(port) {
				return portD.Permit, true
			}
		}
	}
	return false, false
}

func formatSignedProtocol(sP int16) string {
	if sP < 0 {
		return fmt.Sprintf("<%s", reference.GetProtocolName(uint8(-1*sP)))
	}
	return reference.GetProtocolName(uint8(sP))
}

func (p Ports) String() string {
	var s []string

	for signedProtocol, ports := range p {
		var portStrings []string
		for _, port := range ports {
			portStrings = append(portStrings, port.String())
		}

		s = append(s, fmt.Sprintf("%s:[%s]", formatSignedProtocol(signedProtocol), strings.Join(portStrings, ", ")))
	}

	if len(s) == 0 {
		return "None"
	}
	return strings.Join(s, ", ")
}

// Port represents a port range and a verdict.
type Port struct {
	Permit  bool
	Created int64
	Start   uint16
	End     uint16
}

// Matches checks whether a port object matches the given port.
func (p Port) Matches(port uint16) bool {
	if port >= p.Start && port <= p.End {
		return true
	}
	return false
}

func (p Port) String() string {
	var s string

	if p.Permit {
		s += "permit:"
	} else {
		s += "deny:"
	}

	if p.Start == p.End {
		s += strconv.Itoa(int(p.Start))
	} else {
		s += fmt.Sprintf("%d-%d", p.Start, p.End)
	}

	return s
}
