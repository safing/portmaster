package upnpigd

import (
	"strings"

	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/firewall/inspection"
	"github.com/safing/portmaster/firewall/inspection/inspectutils"
	"github.com/safing/portmaster/netenv"
	"github.com/safing/portmaster/network"
	"github.com/safing/portmaster/network/packet"
)

var DeniedSOAPURN = []string{
	"urn:schemas-upnp-org:service:WANIPConnection",
}

type Inspector struct {
	decoder *inspectutils.HTTPRequestDecoder
}

func (*Inspector) Name() string   { return "UPNP/IGD" }
func (*Inspector) Destroy() error { return nil }

func (insp *Inspector) HandleStream(conn *network.Connection, dir network.FlowDirection, data []byte) (network.Verdict, network.VerdictReason, error) {
	req, err := insp.decoder.HandleStream(conn, dir, data)
	log.Errorf("handle stream: req=%v err=%v", req, err)
	if err != nil {
		// This seems not to be a HTTP connection so we can abort
		log.Debugf("%s: aborting inspection as %s does not seem to be HTTP based (%s)", insp.Name(), conn.ID, err)
		return network.VerdictUndeterminable, nil, nil
	}

	if req == nil {
		// we don't have a full HTTP request yet
		return network.VerdictUndecided, nil, nil
	}

	keepAlive := false
	if connHeader := req.Header.Get("Connection"); strings.Contains(connHeader, "keep-alive") {
		keepAlive = true
	}

	action := strings.ToLower(req.Header.Get("SOAPAction"))
	if action != "" {
		for _, a := range DeniedSOAPURN {
			if strings.HasPrefix(action, strings.ToLower(a)) {
				return network.VerdictBlock,
					&inspectutils.Reason{
						Message: "SOAP action blocked",
						Details: map[string]interface{}{
							"SOAPAction": action,
							"URL":        req.URL.String(),
							"Method":     req.Method,
						},
					},
					nil
			}
		}
	}
	if keepAlive {
		// there might be additional HTTP requests so make sure to
		// inspect all of them.
		return network.VerdictUndecided, nil, nil
	}
	// this connection should end immediately so no need to further
	// inspect it
	return network.VerdictUndeterminable, nil, nil
}

func init() {
	inspection.MustRegister(&inspection.Registration{
		Name:  "UPNP/IGD",
		Order: 0,
		Factory: func(conn *network.Connection, pkt packet.Packet) (network.Inspector, error) {
			// if there's no IP then conn is a DNS request and we don't
			// want to check DNS requests ...
			if conn.Entity.IP == nil {
				return nil, nil
			}
			// we only want to check TCP connections
			if conn.IPProtocol != packet.TCP {
				return nil, nil
			}

			targetsIGD := false
			for _, gw := range netenv.Gateways() {
				if conn.Entity.IP.Equal(gw) {
					targetsIGD = true
					break
				}
			}

			// we only care about connections to routers identified in the
			// current network.
			if !targetsIGD {
				return nil, nil
			}

			log.Errorf("enabling upnp/igd inspector")

			return &Inspector{
				decoder: inspectutils.NewHTTPRequestDecoder([]string{"POST", "GET"}),
			}, nil
		},
	})
}

// compile time checks ...
var _ network.StreamHandler = new(Inspector)
