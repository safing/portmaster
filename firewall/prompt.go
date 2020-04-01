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

var (
	mtSaveProfile = "save profile"
)

//nolint:gocognit // FIXME
func prompt(comm *network.Communication, link *network.Link, pkt packet.Packet) {
	nTTL := time.Duration(promptTimeout()) * time.Second

	// first check if there is an existing notification for this.
	// build notification ID
	var nID string
	switch {
	case comm.Direction, comm.Entity.Domain == "": // connection to/from IP
		if pkt == nil {
			log.Error("firewall: could not prompt for incoming/direct connection: missing pkt")
			if link != nil {
				link.Deny("internal error")
			} else {
				comm.Deny("internal error")
			}
			return
		}
		nID = fmt.Sprintf("firewall-prompt-%d-%s-%s", comm.Process().Pid, comm.Scope, pkt.Info().RemoteIP())
	default: // connection to domain
		nID = fmt.Sprintf("firewall-prompt-%d-%s", comm.Process().Pid, comm.Scope)
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
		case comm.Direction: // incoming
			n.Message = fmt.Sprintf("Application %s wants to accept connections from %s (%d/%d)", comm.Process(), link.Entity.IP.String(), link.Entity.Protocol, link.Entity.Port)
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
		case comm.Entity.Domain == "": // direct connection
			n.Message = fmt.Sprintf("Application %s wants to connect to %s (%d/%d)", comm.Process(), link.Entity.IP.String(), link.Entity.Protocol, link.Entity.Port)
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
			if link != nil {
				n.Message = fmt.Sprintf("Application %s wants to connect to %s (%s %d/%d)", comm.Process(), comm.Entity.Domain, link.Entity.IP.String(), link.Entity.Protocol, link.Entity.Port)
			} else {
				n.Message = fmt.Sprintf("Application %s wants to connect to %s", comm.Process(), comm.Entity.Domain)
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
			if link != nil {
				link.Accept("permitted by user")
			} else {
				comm.Accept("permitted by user")
			}
		default: // deny
			if link != nil {
				link.Accept("denied by user")
			} else {
				comm.Accept("denied by user")
			}
		}

		// end here if we won't save the response to the profile
		if !saveResponse {
			return
		}

		// get profile
		p := comm.Process().Profile()

		var ep endpoints.Endpoint
		switch promptResponse {
		case permitDomainAll:
			ep = &endpoints.EndpointDomain{
				EndpointBase: endpoints.EndpointBase{Permitted: true},
				Domain:       "." + comm.Entity.Domain,
			}
		case permitDomainDistinct:
			ep = &endpoints.EndpointDomain{
				EndpointBase: endpoints.EndpointBase{Permitted: true},
				Domain:       comm.Entity.Domain,
			}
		case denyDomainAll:
			ep = &endpoints.EndpointDomain{
				EndpointBase: endpoints.EndpointBase{Permitted: false},
				Domain:       "." + comm.Entity.Domain,
			}
		case denyDomainDistinct:
			ep = &endpoints.EndpointDomain{
				EndpointBase: endpoints.EndpointBase{Permitted: false},
				Domain:       comm.Entity.Domain,
			}
		case permitIP, permitServingIP:
			ep = &endpoints.EndpointIP{
				EndpointBase: endpoints.EndpointBase{Permitted: true},
				IP:           comm.Entity.IP,
			}
		case denyIP, denyServingIP:
			ep = &endpoints.EndpointIP{
				EndpointBase: endpoints.EndpointBase{Permitted: false},
				IP:           comm.Entity.IP,
			}
		default:
			log.Warningf("filter: unknown prompt response: %s", promptResponse)
		}

		switch promptResponse {
		case permitServingIP, denyServingIP:
			p.AddServiceEndpoint(ep.String())
		default:
			p.AddEndpoint(ep.String())
		}

	case <-n.Expired():
		if link != nil {
			link.Deny("no response to prompt")
		} else {
			comm.Deny("no response to prompt")
		}
	}
}
