package compat

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/safing/portbase/log"
	"github.com/safing/portbase/notifications"
	"github.com/safing/portmaster/process"
	"github.com/safing/portmaster/profile"
)

type baseIssue struct {
	id      string             //nolint:structcheck // Inherited.
	title   string             //nolint:structcheck // Inherited.
	message string             //nolint:structcheck // Inherited.
	level   notifications.Type //nolint:structcheck // Inherited.
}

type systemIssue baseIssue

type appIssue baseIssue

var (
	systemIssueNotification     *notifications.Notification
	systemIssueNotificationLock sync.Mutex

	systemIntegrationIssue = &systemIssue{
		id:      "compat:system-integration-issue",
		title:   "Detected System Integration Issue",
		message: "Portmaster detected a problem with its system integration. You can try to restart or reinstall the Portmaster. If that does not help, please report the issue via [GitHub](https://github.com/safing/portmaster/issues) or send a mail to [support@safing.io](mailto:support@safing.io) so we can help you out.",
		level:   notifications.Error,
	}
	systemCompatibilityIssue = &systemIssue{
		id:      "compat:compatibility-issue",
		title:   "Detected Compatibility Issue",
		message: "Portmaster detected that something is interfering with its operation. This could be a VPN, an Anti-Virus or another network protection software. Please check if you are running an incompatible [VPN client](https://docs.safing.io/portmaster/install/status/vpn-compatibility) or [software](https://docs.safing.io/portmaster/install/status/software-compatibility). Otherwise, please report the issue via [GitHub](https://github.com/safing/portmaster/issues) or send a mail to [support@safing.io](mailto:support@safing.io) so we can help you out.",
		level:   notifications.Error,
	}

	secureDNSBypassIssue = &appIssue{
		id:    "compat:secure-dns-bypass-%s",
		title: "Detected %s Bypass Attempt",
		message: `%s is bypassing Portmaster's firewall functions through its Secure DNS resolver. Portmaster can no longer protect or filter connections coming from %s. Disable Secure DNS within %s to restore functionality.  
Rest assured that Portmaster already handles Secure DNS for your whole device.`,
		// TODO: Add this when the new docs page is finished:
		// , or [find out about other options](link to new docs page)
		level: notifications.Warning,
	}
	multiPeerUDPTunnelIssue = &appIssue{
		id:      "compat:multi-peer-udp-tunnel-%s",
		title:   "Detected SPN Incompatibility in %s",
		message: "Portmaster detected that %s is trying to connect to multiple servers via the SPN using a single UDP connection. This is common for technologies such as torrents. Unfortunately, the SPN does not support this feature currently. You can try to change this behavior within the affected app or you could exempt it from using the SPN.",
		level:   notifications.Warning,
	}
)

func (issue *systemIssue) notify(err error) {
	systemIssueNotificationLock.Lock()
	defer systemIssueNotificationLock.Unlock()

	if systemIssueNotification != nil {
		// Ignore duplicate notification.
		if issue.id == systemIssueNotification.EventID {
			return
		}

		// Remove old notification.
		systemIssueNotification.Delete()
	}

	// Create new notification.
	n := &notifications.Notification{
		EventID:      issue.id,
		Type:         issue.level,
		Title:        issue.title,
		Message:      issue.message,
		ShowOnSystem: true,
	}
	notifications.Notify(n)

	systemIssueNotification = n
	n.AttachToModule(module)

	// Report the raw error as module error.
	module.NewErrorMessage("selfcheck", err).Report()
}

func resetSystemIssue() {
	systemIssueNotificationLock.Lock()
	defer systemIssueNotificationLock.Unlock()

	if systemIssueNotification != nil {
		systemIssueNotification.Delete()
	}
	systemIssueNotification = nil
}

func (issue *appIssue) notify(proc *process.Process) {
	// Get profile from process.
	p := proc.Profile().LocalProfile()
	if p == nil {
		return
	}

	// Ignore notifications for unidentified processes.
	if p.ID == profile.UnidentifiedProfileID {
		return
	}

	// Log warning.
	log.Warningf(
		"compat: detected %s issue with %s",
		strings.ReplaceAll(
			strings.TrimPrefix(
				strings.TrimSuffix(issue.id, "-%s"),
				"compat:",
			),
			"-", " ",
		),
		proc.Path,
	)

	// Build message.
	messageAppNameReplaces := make([]interface{}, strings.Count(issue.message, "%s"))
	for i := range messageAppNameReplaces {
		messageAppNameReplaces[i] = p.Name
	}
	message := fmt.Sprintf(issue.message, messageAppNameReplaces...)

	// Check if we already have this notification.
	eventID := fmt.Sprintf(issue.id, p.ID)
	n := notifications.Get(eventID)
	if n != nil {
		return
	}

	// Otherwise, create a new one.
	n = &notifications.Notification{
		EventID:      eventID,
		Type:         issue.level,
		Title:        fmt.Sprintf(issue.title, p.Name),
		Message:      message,
		ShowOnSystem: true,
		AvailableActions: []*notifications.Action{
			{
				ID:   "ack",
				Text: "OK",
			},
		},
	}
	notifications.Notify(n)

	// Set warning on profile.
	module.StartWorker("set app compat warning", func(ctx context.Context) error {
		var changed bool

		func() {
			p.Lock()
			defer p.Unlock()

			if p.Warning != message || time.Now().Add(-1*time.Hour).After(p.WarningLastUpdated) {
				p.Warning = message
				p.WarningLastUpdated = time.Now()
				changed = true
			}
		}()

		if changed {
			return p.Save()
		}
		return nil
	})
}
