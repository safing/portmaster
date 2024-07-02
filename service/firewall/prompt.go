package firewall

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/notifications"
	"github.com/safing/portmaster/service/intel"
	"github.com/safing/portmaster/service/network"
	"github.com/safing/portmaster/service/profile"
	"github.com/safing/portmaster/service/profile/endpoints"
)

const (
	// notification action IDs.
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

func prompt(ctx context.Context, conn *network.Connection) {
	// Create notification.
	n := createPrompt(ctx, conn)
	if n == nil {
		// createPrompt returns nil when no further action should be taken.
		return
	}

	// Add prompt to connection.
	conn.SetPrompt(n)

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
			// Accept
			conn.Accept("allowed via prompt", profile.CfgOptionEndpointsKey)
		case "":
			// Dismissed
			conn.Deny("prompting canceled, waiting for new decision", profile.CfgOptionDefaultActionKey)
		default:
			// Deny
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

// promptIDPrefix is an identifier for privacy filter prompts. This is also used
// in the UI, so don't change!
const promptIDPrefix = "filter:prompt"

func createPrompt(ctx context.Context, conn *network.Connection) (n *notifications.Notification) {
	expires := time.Now().Add(time.Duration(askTimeout()) * time.Second).Unix()

	// Get local profile.
	layeredProfile := conn.Process().Profile()
	if layeredProfile == nil {
		log.Tracer(ctx).Warningf("filter: tried creating prompt for connection without profile")
		return nil
	}
	localProfile := layeredProfile.LocalProfile()
	if localProfile == nil {
		log.Tracer(ctx).Warningf("filter: tried creating prompt for connection without local profile")
		return nil
	}

	// first check if there is an existing notification for this.
	// build notification ID
	var nID string
	switch {
	case conn.Inbound, conn.Entity.Domain == "": // connection to/from IP
		nID = fmt.Sprintf(
			"%s-%s-%v-%s",
			promptIDPrefix,
			localProfile.ID,
			conn.Inbound,
			conn.Entity.IP,
		)
	default: // connection to domain
		nID = fmt.Sprintf(
			"%s-%s-%s",
			promptIDPrefix,
			localProfile.ID,
			conn.Entity.Domain,
		)
	}

	// Only handle one notification at a time.
	promptNotificationCreation.Lock()
	defer promptNotificationCreation.Unlock()

	n = notifications.Get(nID)

	// If there already is a notification, just update the expiry.
	if n != nil {
		// Get notification state and action.
		n.Lock()
		state := n.State
		action := n.SelectedActionID
		n.Unlock()

		// If the notification is still active, extend and return.
		// This can happen because user input (prompts changing the endpoint
		// lists) can happen any time - also between checking the endpoint lists
		// and now.
		if state == notifications.Active {
			n.Update(expires)
			log.Tracer(ctx).Debugf("filter: updated existing prompt notification")
			return n
		}

		// The notification is not active anymore, let's check if there is an
		// action we can perform.
		// If there already is an action defined, we won't be fast enough to
		// receive the action with n.Response(), so we take direct action here.
		if action != "" {
			switch action {
			case allowDomainAll, allowDomainDistinct, allowIP, allowServingIP:
				conn.Accept("allowed via prompt", profile.CfgOptionEndpointsKey)
			default: // deny
				conn.Deny("blocked via prompt", profile.CfgOptionEndpointsKey)
			}
			return nil // Do not take further action.
		}

		// Continue to create a new notification because the previous one is not
		// active and not actionable.
	}

	// Reference relevant data for save function
	entity := conn.Entity
	// Also needed: localProfile

	// Create new notification.
	n = &notifications.Notification{
		EventID:      nID,
		Type:         notifications.Prompt,
		Title:        "Connection Prompt",
		Category:     "Privacy Filter",
		ShowOnSystem: askWithSystemNotifications(),
		EventData: &promptData{
			Entity: entity,
			Profile: promptProfile{
				Source: string(localProfile.Source),
				ID:     localProfile.ID,
				// LinkedPath is used to enhance the display of the prompt in the UI.
				// TODO: Using the process path is a workaround. Find a cleaner solution.
				LinkedPath: conn.Process().Path,
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

// promptSavingLock makes sure that only one prompt is saved at a time.
// Should prompts be persisted in bulk, the next save process might load an
// outdated profile and save it, losing config data.
var promptSavingLock sync.Mutex

func saveResponse(p *profile.Profile, entity *intel.Entity, promptResponse string) error {
	if promptResponse == cancelPrompt {
		return nil
	}

	promptSavingLock.Lock()
	defer promptSavingLock.Unlock()

	// Update the profile if necessary.
	if p.IsOutdated() {
		var err error
		p, err = profile.GetLocalProfile(p.ID, nil, nil)
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
		log.Infof("filter: added incoming rule to profile %s (LP Rev. %d): %q",
			p, p.LayeredProfile().RevisionCnt(), ep.String())
	default:
		p.AddEndpoint(ep.String())
		log.Infof("filter: added outgoing rule to profile %s (LP Rev. %d): %q",
			p, p.LayeredProfile().RevisionCnt(), ep.String())
	}

	return nil
}
