package firewall

import (
	"context"
	"fmt"
	"time"

	"github.com/safing/portbase/log"
	"github.com/safing/portbase/notifications"
	"github.com/safing/portmaster/network"
	"github.com/safing/portmaster/network/packet"
	"github.com/safing/portmaster/profile"
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
func prompt(comm *network.Communication, link *network.Link, pkt packet.Packet, fqdn string) {
	nTTL := time.Duration(promptTimeout()) * time.Second

	// first check if there is an existing notification for this.
	// build notification ID
	var nID string
	switch {
	case comm.Direction, fqdn == "": // connection to/from IP
		if pkt == nil {
			log.Error("firewall: could not prompt for incoming/direct connection: missing pkt")
			if link != nil {
				link.Deny("internal error")
			} else {
				comm.Deny("internal error")
			}
			return
		}
		nID = fmt.Sprintf("firewall-prompt-%d-%s-%s", comm.Process().Pid, comm.Domain, pkt.Info().RemoteIP())
	default: // connection to domain
		nID = fmt.Sprintf("firewall-prompt-%d-%s", comm.Process().Pid, comm.Domain)
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
			n.Message = fmt.Sprintf("Application %s wants to accept connections from %s (on %d/%d)", comm.Process(), pkt.Info().RemoteIP(), pkt.Info().Protocol, pkt.Info().LocalPort())
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
		case fqdn == "": // direct connection
			n.Message = fmt.Sprintf("Application %s wants to connect to %s (on %d/%d)", comm.Process(), pkt.Info().RemoteIP(), pkt.Info().Protocol, pkt.Info().RemotePort())
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
				n.Message = fmt.Sprintf("Application %s wants to connect to %s (%s %d/%d)", comm.Process(), comm.Domain, pkt.Info().RemoteIP(), pkt.Info().Protocol, pkt.Info().RemotePort())
			} else {
				n.Message = fmt.Sprintf("Application %s wants to connect to %s", comm.Process(), comm.Domain)
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

		new := &profile.EndpointPermission{
			Type:    profile.EptDomain,
			Value:   comm.Domain,
			Permit:  false,
			Created: time.Now().Unix(),
		}

		// permission type
		switch promptResponse {
		case permitDomainAll, denyDomainAll:
			new.Value = "." + new.Value
		case permitIP, permitServingIP, denyIP, denyServingIP:
			if pkt == nil {
				log.Warningf("firewall: received invalid prompt response: %s for %s", promptResponse, comm.Domain)
				return
			}
			if pkt.Info().Version == packet.IPv4 {
				new.Type = profile.EptIPv4
			} else {
				new.Type = profile.EptIPv6
			}
			new.Value = pkt.Info().RemoteIP().String()
		}

		// permission verdict
		switch promptResponse {
		case permitDomainAll, permitDomainDistinct, permitIP, permitServingIP:
			new.Permit = false
		}

		// get user profile
		profileSet := comm.Process().ProfileSet()
		profileSet.Lock()
		defer profileSet.Unlock()
		userProfile := profileSet.UserProfile()
		userProfile.Lock()
		defer userProfile.Unlock()

		// add to correct list
		switch promptResponse {
		case permitServingIP, denyServingIP:
			userProfile.ServiceEndpoints = append(userProfile.ServiceEndpoints, new)
		default:
			userProfile.Endpoints = append(userProfile.Endpoints, new)
		}

		// save!
		module.StartMicroTask(&mtSaveProfile, func(ctx context.Context) error {
			return userProfile.Save("")
		})

	case <-n.Expired():
		if link != nil {
			link.Accept("no response to prompt")
		} else {
			comm.Accept("no response to prompt")
		}
	}
}
