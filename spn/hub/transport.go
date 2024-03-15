package hub

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"golang.org/x/exp/slices"
)

// Examples:
// "spn:17",
// "smtp:25",
// "smtp:587",
// "imap:143",
// "http:80",
// "http://example.com:80/example", // HTTP (based): use full path for request
// "https:443",
// "ws:80",
// "wss://example.com:443/spn",

// Transport represents a "endpoint" that others can connect to. This allows for use of different protocols, ports and infrastructure integration.
type Transport struct {
	Protocol string
	Domain   string
	Port     uint16
	Path     string
	Option   string
}

// ParseTransports returns a list of parsed transports and errors from parsing
// the given definitions.
func ParseTransports(definitions []string) (transports []*Transport, errs []error) {
	transports = make([]*Transport, 0, len(definitions))
	for _, definition := range definitions {
		parsed, err := ParseTransport(definition)
		if err != nil {
			errs = append(errs, fmt.Errorf(
				"unknown or invalid transport %q: %w", definition, err,
			))
		} else {
			transports = append(transports, parsed)
		}
	}

	SortTransports(transports)
	return transports, errs
}

// ParseTransport parses a transport definition.
func ParseTransport(definition string) (*Transport, error) {
	u, err := url.Parse(definition)
	if err != nil {
		return nil, err
	}

	// check for invalid parts
	if u.User != nil {
		return nil, errors.New("user/pass is not allowed")
	}

	// put into transport
	t := &Transport{
		Protocol: u.Scheme,
		Domain:   u.Hostname(),
		Path:     u.RequestURI(),
		Option:   u.Fragment,
	}

	// parse port
	portData := u.Port()
	if portData == "" {
		// no port available - it might be in u.Opaque, which holds both the port and possibly a path
		portData = strings.SplitN(u.Opaque, "/", 2)[0] // get port
		t.Path = strings.TrimPrefix(t.Path, portData)  // trim port from path
		// check again for port
		if portData == "" {
			return nil, errors.New("missing port")
		}
	}
	port, err := strconv.ParseUint(portData, 10, 16)
	if err != nil {
		return nil, errors.New("invalid port")
	}
	t.Port = uint16(port)

	// check port
	if t.Port == 0 {
		return nil, errors.New("invalid port")
	}

	// remove root paths
	if t.Path == "/" {
		t.Path = ""
	}

	// check for protocol
	if t.Protocol == "" {
		return nil, errors.New("missing scheme/protocol")
	}

	return t, nil
}

// String returns the definition form of the transport.
func (t *Transport) String() string {
	switch {
	case t.Option != "":
		return fmt.Sprintf("%s://%s:%d%s#%s", t.Protocol, t.Domain, t.Port, t.Path, t.Option)
	case t.Domain != "":
		return fmt.Sprintf("%s://%s:%d%s", t.Protocol, t.Domain, t.Port, t.Path)
	default:
		return fmt.Sprintf("%s:%d%s", t.Protocol, t.Port, t.Path)
	}
}

// SortTransports sorts the transports to emphasize certain protocols, but
// otherwise leaves the order intact.
func SortTransports(ts []*Transport) {
	slices.SortStableFunc[[]*Transport, *Transport](ts, func(a, b *Transport) int {
		aOrder := a.protocolOrder()
		bOrder := b.protocolOrder()

		switch {
		case aOrder != bOrder:
			return aOrder - bOrder
		// case a.Port != b.Port:
		// 	return int(a.Port) - int(b.Port)
		// case a.Domain != b.Domain:
		// 	return strings.Compare(a.Domain, b.Domain)
		// case a.Path != b.Path:
		// 	return strings.Compare(a.Path, b.Path)
		// case a.Option != b.Option:
		// 	return strings.Compare(a.Option, b.Option)
		default:
			return 0
		}
	})
}

func (t *Transport) protocolOrder() int {
	switch t.Protocol {
	case "http":
		return 1
	case "spn":
		return 2
	default:
		return 100
	}
}
