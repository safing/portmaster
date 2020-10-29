package firewall

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/safing/portbase/log"
	"github.com/safing/portbase/notifications"
	"github.com/safing/portmaster/intel"
	"github.com/safing/portmaster/network"
	"github.com/safing/portmaster/network/packet"
	"github.com/safing/portmaster/profile"
	"github.com/safing/portmaster/profile/endpoints"
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
	promptNotificationCreation sync.Mutex
)

type promptData struct {
	Entity  *intel.Entity
	Profile promptProfile
}

type promptProfile struct {
	Source     string
	ID         string
	LinkedPath string
}

func prompt(ctx context.Context, conn *network.Connection, pkt packet.Packet) { //nolint:gocognit // TODO
	// Create notification.
	n := createPrompt(ctx, conn, pkt)

	// wait for response/timeout
	select {
	case promptResponse := <-n.Response():
		switch promptResponse {
		case permitDomainAll, permitDomainDistinct, permitIP, permitServingIP:
			conn.Accept("permitted via prompt", profile.CfgOptionEndpointsKey)
		default: // deny
			conn.Deny("blocked via prompt", profile.CfgOptionEndpointsKey)
		}

	case <-time.After(1 * time.Second):
		log.Tracer(ctx).Debugf("filter: continueing prompting async")
		conn.Deny("prompting in progress", profile.CfgOptionDefaultActionKey)

	case <-ctx.Done():
		log.Tracer(ctx).Debugf("filter: aborting prompting because of shutdown")
		conn.Drop("shutting down", noReasonOptionKey)
	}
}

func createPrompt(ctx context.Context, conn *network.Connection, pkt packet.Packet) (n *notifications.Notification) {
	expires := time.Now().Add(time.Duration(askTimeout()) * time.Second).Unix()

	// first check if there is an existing notification for this.
	// build notification ID
	var nID string
	switch {
	case conn.Inbound, conn.Entity.Domain == "": // connection to/from IP
		nID = fmt.Sprintf("filter:prompt-%d-%s-%s", conn.Process().Pid, conn.Scope, pkt.Info().RemoteIP())
	default: // connection to domain
		nID = fmt.Sprintf("filter:prompt-%d-%s", conn.Process().Pid, conn.Scope)
	}

	// Only handle one notification at a time.
	promptNotificationCreation.Lock()
	defer promptNotificationCreation.Unlock()

	n = notifications.Get(nID)

	// If there already is a notification, just update the expiry.
	if n != nil {
		n.Update(expires)
		log.Tracer(ctx).Debugf("filter: updated existing prompt notification")
		return
	}

	// Reference relevant data for save function
	localProfile := conn.Process().Profile().LocalProfile()
	entity := conn.Entity

	// Create new notification.
	n = &notifications.Notification{
		EventID:  nID,
		Type:     notifications.Prompt,
		Title:    "Connection Prompt",
		Category: "Privacy Filter",
		EventData: &promptData{
			Entity: entity,
			Profile: promptProfile{
				Source:     string(localProfile.Source),
				ID:         localProfile.ID,
				LinkedPath: localProfile.LinkedPath,
			},
		},
		Expires: expires,
	}

	// Set action function.
	n.SetActionFunction(func(_ context.Context, n *notifications.Notification) error {
		return saveResponse(
			localProfile,
			entity,
			n.SelectedActionID,
		)
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
		n.Message = fmt.Sprintf("Application %s wants to connect to %s", conn.Process(), conn.Entity.Domain)
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

	n.Save()
	log.Tracer(ctx).Debugf("filter: sent prompt notification")

	return n
}

func saveResponse(p *profile.Profile, entity *intel.Entity, promptResponse string) error {
	// Update the profile if necessary.
	if p.IsOutdated() {
		var err error
		p, _, err = profile.GetProfile(p.Source, p.ID, p.LinkedPath)
		if err != nil {
			return err
		}
	}

	var ep endpoints.Endpoint
	switch promptResponse {
	case permitDomainAll:
		ep = &endpoints.EndpointDomain{
			EndpointBase:  endpoints.EndpointBase{Permitted: true},
			OriginalValue: "." + entity.Domain,
		}
	case permitDomainDistinct:
		ep = &endpoints.EndpointDomain{
			EndpointBase:  endpoints.EndpointBase{Permitted: true},
			OriginalValue: entity.Domain,
		}
	case denyDomainAll:
		ep = &endpoints.EndpointDomain{
			EndpointBase:  endpoints.EndpointBase{Permitted: false},
			OriginalValue: "." + entity.Domain,
		}
	case denyDomainDistinct:
		ep = &endpoints.EndpointDomain{
			EndpointBase:  endpoints.EndpointBase{Permitted: false},
			OriginalValue: entity.Domain,
		}
	case permitIP, permitServingIP:
		ep = &endpoints.EndpointIP{
			EndpointBase: endpoints.EndpointBase{Permitted: true},
			IP:           entity.IP,
		}
	case denyIP, denyServingIP:
		ep = &endpoints.EndpointIP{
			EndpointBase: endpoints.EndpointBase{Permitted: false},
			IP:           entity.IP,
		}
	default:
		return fmt.Errorf("unknown prompt response: %s", promptResponse)
	}

	switch promptResponse {
	case permitServingIP, denyServingIP:
		p.AddServiceEndpoint(ep.String())
		log.Infof("filter: added incoming rule to profile %s: %q", p, ep.String())
	default:
		p.AddEndpoint(ep.String())
		log.Infof("filter: added outgoing rule to profile %s: %q", p, ep.String())
	}

	return nil
}
