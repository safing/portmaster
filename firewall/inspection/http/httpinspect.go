package http

import (
	"context"

	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/firewall/inspection"
	"github.com/safing/portmaster/firewall/inspection/inspectutils"
	"github.com/safing/portmaster/intel"
	"github.com/safing/portmaster/intel/filterlists"
	"github.com/safing/portmaster/network"
	"github.com/safing/portmaster/network/packet"
	"github.com/safing/portmaster/profile/endpoints"
)

// Inspector implements a simple plain HTTP inspector.
type Inspector struct {
	decoder *inspectutils.HTTPRequestDecoder
}

func (inspector *Inspector) HandleStream(conn *network.Connection, dir network.FlowDirection, data []byte) (network.Verdict, network.VerdictReason, error) {
	req, err := inspector.decoder.HandleStream(conn, dir, data)
	if err != nil {
		// we're done here ...
		return network.VerdictUndeterminable, nil, err
	}
	if req == nil {
		// we don't have a full request yet
		return network.VerdictUndecided, nil, nil
	}

	if conn.Entity.Domain != req.Host {
		ctx := context.TODO()
		lp := conn.Process().Profile()
		log.Infof("%s found domain, re-evaluating connection from %s to %s", lp.Key(), conn.Entity.Domain, req.Host)

		e := &intel.Entity{
			Domain: req.Host + ".",
			IP:     conn.Entity.IP,
			Port:   conn.Entity.Port,
		}
		e.ResolveSubDomainLists(ctx, lp.FilterSubDomains())
		e.EnableCNAMECheck(ctx, lp.FilterCNAMEs())
		e.LoadLists(context.TODO())

		d, err := filterlists.LookupDomain(e.Domain)
		log.Infof("found entity in lists: %v - %v (%v - %v)", e.BlockedByLists, e.ListsError, err, d)

		result, reason := lp.MatchFilterLists(ctx, e)
		switch result {
		case endpoints.Denied:
			log.Infof("connection blocked ...")
			return network.VerdictBlock, reason, nil
		}
	}
	return network.VerdictUndeterminable, nil, nil
}

// TODO(ppacher): get rid of this function, we already specify the name
// in inspection:RegisterInspector ...
func (*Inspector) Name() string {
	return "HTTP"
}

// Destory does nothing ...
func (*Inspector) Destroy() error {
	return nil
}

func init() {
	inspection.MustRegister(&inspection.Registration{
		Name:  "HTTP",
		Order: 1, // we don't actually care
		Factory: func(conn *network.Connection, pkt packet.Packet) (network.Inspector, error) {
			// we only support outgoing http clients for now ...
			if conn.Entity.DstPort() != 80 {
				return nil, nil
			}

			insp := &Inspector{
				decoder: inspectutils.NewHTTPRequestDecoder(inspectutils.HTTPMethods),
			}

			return insp, nil
		},
	})
}

// compile time check if Inspector corretly satisfies all
// required interfaces ...
var _ network.StreamHandler = new(Inspector)
