package profile

import (
	"fmt"
	"strconv"
)

// Endpoints is a list of permitted or denied endpoints.
type Endpoints []*EndpointPermission

// EndpointPermission holds a decision about an endpoint.
type EndpointPermission struct {
	DomainOrIP        string
	IncludeSubdomains bool
	Protocol          uint8
	PortStart         uint16
	PortEnd           uint16
	Permit            bool
	Created           int64
}

// IsSet returns whether the Endpoints object is "set".
func (e Endpoints) IsSet() bool {
	if len(e) > 0 {
		return true
	}
	return false
}

// Check checks if the given domain is governed in the list of domains and returns whether it is permitted.
func (e Endpoints) Check(domainOrIP string, protocol uint8, port uint16) (permit, ok bool) {
	// check for exact domain
	ed, ok := d[domain]
	if ok {
		return ed.Permit, true
	}

	for _, entry := range e {
		if entry.Matches(domainOrIP, protocol, port) {
			return entry.Permit, true
		}
	}

	return false, false
}

// Matches checks whether a port object matches the given port.
func (ep EndpointPermission) Matches(domainOrIP string, protocol uint8, port uint16) bool {
	if domainOrIP != ep.DomainOrIP {
		return false
	}

	if ep.Protocol > 0 && protocol != ep.Protocol {
		return false
	}

	if ep.PortStart > 0 && (port < ep.PortStart || port > ep.PortEnd) {
		return false
	}

	return true
}

func (ep EndpointPermission) String() string {
	s := ep.DomainOrIP

	if ep.Protocol > 0 || ep.Start {
		s += " "
	}

	if ep.Protocol > 0 {
		s += strconv.Itoa(int(ep.Protocol))
		if ep.Start > 0 {
			s += "/"
		}
	}

	if ep.Start > 0 {
		if p.Start == p.End {
			s += strconv.Itoa(int(ep.Start))
		} else {
			s += fmt.Sprintf("%d-%d", ep.Start, ep.End)
		}
	}

	return s
}
