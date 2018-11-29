package profile

import (
	"fmt"
	"strconv"
	"strings"
)

// Ports is a list of permitted or denied ports
type Ports map[string][]*Port

// Check returns whether listening/connecting to a certain port is allowed, if set.
func (p Ports) Check(listen bool, protocol string, port uint16) (permit, ok bool) {
	if p == nil {
		return false, false
	}

	if listen {
		protocol = "<" + protocol
	}
	portDefinitions, ok := p[protocol]
	if ok {
		for _, portD := range portDefinitions {
			if portD.Matches(port) {
				return portD.Permit, true
			}
		}
	}
	return false, false
}

func (p Ports) String() string {
	var s []string

	for protocol, ports := range p {
		var portStrings []string
		for _, port := range ports {
			portStrings = append(portStrings, port.String())
		}

		s = append(s, fmt.Sprintf("%s:[%s]", protocol, strings.Join(portStrings, ", ")))
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
