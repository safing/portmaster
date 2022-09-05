package notification

import (
	"context"

	"github.com/safing/portmaster/plugin/shared/proto"
)

type (
	// Service provides access to the Portmaster Notification system.
	Service interface {
		// CreateNotification creates a new notification and returns
		// a channel that receives the selected action id.
		// If no actions are defined for the notification the returned
		// channel will be closed immediately.
		//
		// Note that notifications with custom actions are bound to the lifetime of a plugin
		// and will be removed as soon as the plugin exits.
		CreateNotification(context.Context, *proto.Notification) (<-chan string, error)
	}
)
