package firewall

import (
	"fmt"
	"time"

	"github.com/safing/portmaster/profile/endpoints"

	"github.com/safing/portbase/log"
	"github.com/safing/portbase/notifications"
	"github.com/safing/portmaster/network"
	"github.com/safing/portmaster/network/packet"
)

const (
	// notification action IDs
	permitDomainAll      = "permit-domain-all"
	permitDomainDistinct = "permit-domain-distinct"
	denyDomainAll        = "deny-domain-all"
	denyDomainDistinct   = "deny-domain-distinct"

	permitIP        = "permit-ip"
	denyIP          = "deny-ip"
	permitServingIP = "permit-serving-ip"
	denyServingIP   = "deny-serving-ip"
)

func prompt(conn *network.Connection, pkt packet.Packet) { //nolint:gocognit // TODO
	nTTL := time.Duration(askTimeout()) * time.Second

	// first check if there is an existing notification for this.
	// build notification ID
	var nID string
	switch {
	case conn.Inbound, conn.Entity.Domain == "": // connection to/from IP
		nID = fmt.Sprintf("filter:prompt-%d-%s-%s", conn.Process().Pid, conn.Scope, pkt.Info().RemoteIP())
	default: // connection to domain
		nID = fmt.Sprintf("filter:prompt-%d-%s", conn.Process().Pid, conn.Scope)
	}
	n := notifications.Get(nID)
	saveResponse := true

	if n != nil {
		//  update with new expiry
		n.Update(time.Now().Add(nTTL).Unix())
		// do not save response to profile
		saveResponse = false
	} else {
		// create new notification
		n = (&notifications.Notification{
			ID:      nID,
			Type:    notifications.Prompt,
			Expires: time.Now().Add(nTTL).Unix(),
		})

		// add message and actions
		switch {
		case conn.Inbound:
			n.Message = fmt.Sprintf("Application %s wants to accept connections from %s (%d/%d)", conn.Process(), conn.Entity.IP.String(), conn.Entity.Protocol, conn.Entity.Port)
			n.AvailableActions = []*notifications.Action{
				{
					ID:   permitServingIP,
					Text: "Permit",
				},
				{
					ID:   denyServingIP,
					Text: "Deny",
				},
			}
		case conn.Entity.Domain == "": // direct connection
			n.Message = fmt.Sprintf("Application %s wants to connect to %s (%d/%d)", conn.Process(), conn.Entity.IP.String(), conn.Entity.Protocol, conn.Entity.Port)
			n.AvailableActions = []*notifications.Action{
				{
					ID:   permitIP,
					Text: "Permit",
				},
				{
					ID:   denyIP,
					Text: "Deny",
				},
			}
		default: // connection to domain
			if pkt != nil {
				n.Message = fmt.Sprintf("Application %s wants to connect to %s (%s %d/%d)", conn.Process(), conn.Entity.Domain, conn.Entity.IP.String(), conn.Entity.Protocol, conn.Entity.Port)
			} else {
				n.Message = fmt.Sprintf("Application %s wants to connect to %s", conn.Process(), conn.Entity.Domain)
			}
			n.AvailableActions = []*notifications.Action{
				{
					ID:   permitDomainAll,
					Text: "Permit all",
				},
				{
					ID:   permitDomainDistinct,
					Text: "Permit",
				},
				{
					ID:   denyDomainDistinct,
					Text: "Deny",
				},
			}
		}
		// save new notification
		n.Save()
	}

	// wait for response/timeout
	select {
	case promptResponse := <-n.Response():
		switch promptResponse {
		case permitDomainAll, permitDomainDistinct, permitIP, permitServingIP:
			conn.Accept("permitted by user")
		default: // deny
			conn.Deny("denied by user")
		}

		// end here if we won't save the response to the profile
		if !saveResponse {
			return
		}

		// get profile
		p := conn.Process().Profile()

		var ep endpoints.Endpoint
		switch promptResponse {
		case permitDomainAll:
			ep = &endpoints.EndpointDomain{
				EndpointBase: endpoints.EndpointBase{Permitted: true},
				Domain:       "." + conn.Entity.Domain,
			}
		case permitDomainDistinct:
			ep = &endpoints.EndpointDomain{
				EndpointBase: endpoints.EndpointBase{Permitted: true},
				Domain:       conn.Entity.Domain,
			}
		case denyDomainAll:
			ep = &endpoints.EndpointDomain{
				EndpointBase: endpoints.EndpointBase{Permitted: false},
				Domain:       "." + conn.Entity.Domain,
			}
		case denyDomainDistinct:
			ep = &endpoints.EndpointDomain{
				EndpointBase: endpoints.EndpointBase{Permitted: false},
				Domain:       conn.Entity.Domain,
			}
		case permitIP, permitServingIP:
			ep = &endpoints.EndpointIP{
				EndpointBase: endpoints.EndpointBase{Permitted: true},
				IP:           conn.Entity.IP,
			}
		case denyIP, denyServingIP:
			ep = &endpoints.EndpointIP{
				EndpointBase: endpoints.EndpointBase{Permitted: false},
				IP:           conn.Entity.IP,
			}
		default:
			log.Warningf("filter: unknown prompt response: %s", promptResponse)
			return
		}

		switch promptResponse {
		case permitServingIP, denyServingIP:
			p.AddServiceEndpoint(ep.String())
		default:
			p.AddEndpoint(ep.String())
		}

	case <-n.Expired():
		conn.Deny("no response to prompt")
	}
}
