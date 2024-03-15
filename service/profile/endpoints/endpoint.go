package endpoints

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/safing/portmaster/service/intel"
	"github.com/safing/portmaster/service/network/reference"
)

// Endpoint describes an Endpoint Matcher.
type Endpoint interface {
	Matches(ctx context.Context, entity *intel.Entity) (EPResult, Reason)
	String() string
}

// EndpointBase provides general functions for implementing an Endpoint to reduce boilerplate.
type EndpointBase struct { //nolint:maligned // TODO
	Protocol  uint8
	StartPort uint16
	EndPort   uint16

	Permitted bool
}

func (ep *EndpointBase) match(s fmt.Stringer, entity *intel.Entity, value, desc string, keyval ...interface{}) (EPResult, Reason) {
	result := ep.matchesPPP(entity)
	if result == NoMatch {
		return result, nil
	}

	return result, ep.makeReason(s, value, desc, keyval...)
}

func (ep *EndpointBase) makeReason(s fmt.Stringer, value, desc string, keyval ...interface{}) Reason {
	r := &reason{
		description: desc,
		Filter:      s.String(),
		Permitted:   ep.Permitted,
		Value:       value,
	}

	r.Extra = make(map[string]interface{})

	for idx := 0; idx < len(keyval)/2; idx += 2 {
		key := keyval[idx]
		val := keyval[idx+1]

		if keyName, ok := key.(string); ok {
			r.Extra[keyName] = val
		}
	}

	return r
}

func (ep *EndpointBase) matchesPPP(entity *intel.Entity) (result EPResult) {
	// only check if protocol is defined
	if ep.Protocol > 0 {
		// if protocol does not match, return NoMatch
		if entity.Protocol != ep.Protocol {
			return NoMatch
		}
	}

	// only check if port is defined
	if ep.StartPort > 0 {
		// if port does not match, return NoMatch
		if entity.DstPort() < ep.StartPort || entity.DstPort() > ep.EndPort {
			return NoMatch
		}
	}

	// protocol and port matched or were defined as any
	if ep.Permitted {
		return Permitted
	}
	return Denied
}

func (ep *EndpointBase) renderPPP(s string) string {
	var rendered string
	if ep.Permitted {
		rendered = "+ " + s
	} else {
		rendered = "- " + s
	}

	if ep.Protocol > 0 || ep.StartPort > 0 {
		if ep.Protocol > 0 {
			rendered += " " + reference.GetProtocolName(ep.Protocol)
		} else {
			rendered += " *"
		}

		if ep.StartPort > 0 {
			if ep.StartPort == ep.EndPort {
				rendered += "/" + reference.GetPortName(ep.StartPort)
			} else {
				rendered += "/" + strconv.Itoa(int(ep.StartPort)) + "-" + strconv.Itoa(int(ep.EndPort))
			}
		}
	}

	return rendered
}

func (ep *EndpointBase) parsePPP(typedEp Endpoint, fields []string) (Endpoint, error) { //nolint:gocognit // TODO
	switch len(fields) {
	case 2:
		// nothing else to do here
	case 3:
		// parse protocol and port(s)
		var ok bool
		splitted := strings.Split(fields[2], "/")
		if len(splitted) > 2 {
			return nil, invalidDefinitionError(fields, "protocol and port must be in format <protocol>/<port>")
		}
		// protocol
		switch splitted[0] {
		case "":
			return nil, invalidDefinitionError(fields, "protocol can't be empty")
		case "*":
			// any protocol that supports ports
		default:
			n, err := strconv.ParseUint(splitted[0], 10, 8)
			n8 := uint8(n)
			if err != nil {
				// maybe it's a name?
				n8, ok = reference.GetProtocolNumber(splitted[0])
				if !ok {
					return nil, invalidDefinitionError(fields, "protocol number parsing error")
				}
			}
			ep.Protocol = n8
		}
		// port(s)
		if len(splitted) > 1 {
			switch splitted[1] {
			case "", "*":
				return nil, invalidDefinitionError(fields, "omit port if should match any")
			default:
				portSplitted := strings.Split(splitted[1], "-")
				if len(portSplitted) > 2 {
					return nil, invalidDefinitionError(fields, "ports must be in format from-to")
				}
				// parse start port
				n, err := strconv.ParseUint(portSplitted[0], 10, 16)
				n16 := uint16(n)
				if err != nil {
					// maybe it's a name?
					n16, ok = reference.GetPortNumber(portSplitted[0])
					if !ok {
						return nil, invalidDefinitionError(fields, "port number parsing error")
					}
				}
				if n16 == 0 {
					return nil, invalidDefinitionError(fields, "port number cannot be 0")
				}
				ep.StartPort = n16
				// parse end port
				if len(portSplitted) > 1 {
					n, err = strconv.ParseUint(portSplitted[1], 10, 16)
					n16 = uint16(n)
					if err != nil {
						// maybe it's a name?
						n16, ok = reference.GetPortNumber(portSplitted[1])
						if !ok {
							return nil, invalidDefinitionError(fields, "port number parsing error")
						}
					}
				}
				if n16 == 0 {
					return nil, invalidDefinitionError(fields, "port number cannot be 0")
				}
				ep.EndPort = n16
			}
		}
		// check if anything was parsed
		if ep.Protocol == 0 && ep.StartPort == 0 {
			return nil, invalidDefinitionError(fields, "omit protocol/port if should match any")
		}
	default:
		return nil, invalidDefinitionError(fields, "there should be only 2 or 3 segments")
	}

	switch fields[0] {
	case "+":
		ep.Permitted = true
	case "-":
		ep.Permitted = false
	default:
		return nil, invalidDefinitionError(fields, "invalid permission prefix")
	}

	return typedEp, nil
}

func invalidDefinitionError(fields []string, msg string) error {
	return fmt.Errorf(`invalid endpoint definition: "%s" - %s`, strings.Join(fields, " "), msg)
}

//nolint:gocognit,nakedret
func parseEndpoint(value string) (endpoint Endpoint, err error) {
	fields := strings.Fields(value)
	if len(fields) < 2 {
		return nil, fmt.Errorf(`invalid endpoint definition: "%s"`, value)
	}

	// Remove comment.
	for i, field := range fields {
		if strings.HasPrefix(field, "#") {
			fields = fields[:i]
			break
		}
	}

	// any
	if endpoint, err = parseTypeAny(fields); endpoint != nil || err != nil {
		return
	}
	// ip
	if endpoint, err = parseTypeIP(fields); endpoint != nil || err != nil {
		return
	}
	// ip range
	if endpoint, err = parseTypeIPRange(fields); endpoint != nil || err != nil {
		return
	}
	// country
	if endpoint, err = parseTypeCountry(fields); endpoint != nil || err != nil {
		return
	}
	// continent
	if endpoint, err = parseTypeContinent(fields); endpoint != nil || err != nil {
		return
	}
	// asn
	if endpoint, err = parseTypeASN(fields); endpoint != nil || err != nil {
		return
	}
	// scopes
	if endpoint, err = parseTypeScope(fields); endpoint != nil || err != nil {
		return
	}
	// lists
	if endpoint, err = parseTypeList(fields); endpoint != nil || err != nil {
		return
	}
	// domain
	if endpoint, err = parseTypeDomain(fields); endpoint != nil || err != nil {
		return
	}

	return nil, fmt.Errorf(`unknown endpoint definition: "%s"`, value)
}
