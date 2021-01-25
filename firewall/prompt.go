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
	allowDomainAll      = "allow-domain-all"
	allowDomainDistinct = "allow-domain-distinct"
	blockDomainAll      = "block-domain-all"
	blockDomainDistinct = "block-domain-distinct"

	allowIP        = "allow-ip"
	blockIP        = "block-ip"
	allowServingIP = "allow-serving-ip"
	blockServingIP = "block-serving-ip"

	cancelPrompt = "cancel"
)

var (
	promptNotificationCreation sync.Mutex

	decisionTimeout int64 = 10 // in seconds
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

func prompt(ctx context.Context, conn *network.Connection, pkt packet.Packet) {
	// Create notification.
	n := createPrompt(ctx, conn, pkt)

	// Get decision timeout and make sure it does not exceed the ask timeout.
	timeout := decisionTimeout
	if timeout > askTimeout() {
		timeout = askTimeout()
	}

	// wait for response/timeout
	select {
	case promptResponse := <-n.Response():
		switch promptResponse {
		case allowDomainAll, allowDomainDistinct, allowIP, allowServingIP:
			conn.Accept("permitted via prompt", profile.CfgOptionEndpointsKey)
		default: // deny
			conn.Deny("blocked via prompt", profile.CfgOptionEndpointsKey)
		}

	case <-time.After(time.Duration(timeout) * time.Second):
		log.Tracer(ctx).Debugf("filter: continuing prompting async")
		conn.Deny("prompting in progress, please respond to prompt", profile.CfgOptionDefaultActionKey)

	case <-ctx.Done():
		log.Tracer(ctx).Debugf("filter: aborting prompting because of shutdown")
		conn.Drop("shutting down", noReasonOptionKey)
	}
}

// promptIDPrefix is an identifier for privacy filter prompts. This is also use
// in the UI, so don't change!
const promptIDPrefix = "filter:prompt"

func createPrompt(ctx context.Context, conn *network.Connection, pkt packet.Packet) (n *notifications.Notification) {
	expires := time.Now().Add(time.Duration(askTimeout()) * time.Second).Unix()

	// Get local profile.
	profile := conn.Process().Profile()
	if profile == nil {
		log.Tracer(ctx).Warningf("filter: tried creating prompt for connection without profile")
		return
	}
	localProfile := profile.LocalProfile()
	if localProfile == nil {
		log.Tracer(ctx).Warningf("filter: tried creating prompt for connection without local profile")
		return
	}

	// first check if there is an existing notification for this.
	// build notification ID
	var nID string
	switch {
	case conn.Inbound, conn.Entity.Domain == "": // connection to/from IP
		nID = fmt.Sprintf(
			"%s-%s-%s-%s",
			promptIDPrefix,
			localProfile.ID,
			conn.Scope,
			pkt.Info().RemoteIP(),
		)
	default: // connection to domain
		nID = fmt.Sprintf(
			"%s-%s-%s",
			promptIDPrefix,
			localProfile.ID,
			conn.Scope,
		)
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
	entity := conn.Entity
	// Also needed: localProfile

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

	// Get name of profile for notification. The profile is read-locked by the firewall handler.
	profileName := localProfile.Name

	// add message and actions
	switch {
	case conn.Inbound:
		n.Message = fmt.Sprintf("%s wants to accept connections from %s (%d/%d)", profileName, conn.Entity.IP.String(), conn.Entity.Protocol, conn.Entity.Port)
		n.AvailableActions = []*notifications.Action{
			{
				ID:   allowServingIP,
				Text: "Allow",
			},
			{
				ID:   blockServingIP,
				Text: "Block",
			},
		}
	case conn.Entity.Domain == "": // direct connection
		n.Message = fmt.Sprintf("%s wants to connect to %s (%d/%d)", profileName, conn.Entity.IP.String(), conn.Entity.Protocol, conn.Entity.Port)
		n.AvailableActions = []*notifications.Action{
			{
				ID:   allowIP,
				Text: "Allow",
			},
			{
				ID:   blockIP,
				Text: "Block",
			},
		}
	default: // connection to domain
		n.Message = fmt.Sprintf("%s wants to connect to %s", profileName, conn.Entity.Domain)
		n.AvailableActions = []*notifications.Action{
			{
				ID:   allowDomainAll,
				Text: "Allow",
			},
			{
				ID:   blockDomainAll,
				Text: "Block",
			},
		}
	}

	n.Save()
	log.Tracer(ctx).Debugf("filter: sent prompt notification")

	return n
}

func saveResponse(p *profile.Profile, entity *intel.Entity, promptResponse string) error {
	if promptResponse == cancelPrompt {
		return nil
	}

	// Update the profile if necessary.
	if p.IsOutdated() {
		var err error
		p, err = profile.GetProfile(p.Source, p.ID, p.LinkedPath)
		if err != nil {
			return err
		}
	}

	var ep endpoints.Endpoint
	switch promptResponse {
	case allowDomainAll:
		ep = &endpoints.EndpointDomain{
			EndpointBase:  endpoints.EndpointBase{Permitted: true},
			OriginalValue: "." + entity.Domain,
		}
	case allowDomainDistinct:
		ep = &endpoints.EndpointDomain{
			EndpointBase:  endpoints.EndpointBase{Permitted: true},
			OriginalValue: entity.Domain,
		}
	case blockDomainAll:
		ep = &endpoints.EndpointDomain{
			EndpointBase:  endpoints.EndpointBase{Permitted: false},
			OriginalValue: "." + entity.Domain,
		}
	case blockDomainDistinct:
		ep = &endpoints.EndpointDomain{
			EndpointBase:  endpoints.EndpointBase{Permitted: false},
			OriginalValue: entity.Domain,
		}
	case allowIP, allowServingIP:
		ep = &endpoints.EndpointIP{
			EndpointBase: endpoints.EndpointBase{Permitted: true},
			IP:           entity.IP,
		}
	case blockIP, blockServingIP:
		ep = &endpoints.EndpointIP{
			EndpointBase: endpoints.EndpointBase{Permitted: false},
			IP:           entity.IP,
		}
	case cancelPrompt:
		return nil
	default:
		return fmt.Errorf("unknown prompt response: %s", promptResponse)
	}

	switch promptResponse {
	case allowServingIP, blockServingIP:
		p.AddServiceEndpoint(ep.String())
		log.Infof("filter: added incoming rule to profile %s: %q", p, ep.String())
	default:
		p.AddEndpoint(ep.String())
		log.Infof("filter: added outgoing rule to profile %s: %q", p, ep.String())
	}

	return nil
}
