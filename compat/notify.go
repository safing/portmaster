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
)

type baseIssue struct {
	id      string
	title   string
	message string
	level   notifications.Type
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
		id:      "compat:secure-dns-bypass-%s",
		title:   "Detected %s Bypass Attempt",
		message: "Portmaster detected that %s is trying to use a secure DNS resolver. While this is a good thing, the Portmaster already handles secure DNS for your whole device. Please disable the secure DNS resolver within the app.",
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

func (issue *appIssue) notify(p *process.Process) {
	// Get profile from process.
	profile := p.Profile().LocalProfile()
	if profile == nil {
		return
	}

	// Log warning.
	log.Warningf(
		"compat: detected %s issue with %s",
		strings.ReplaceAll(
			strings.TrimPrefix(
				strings.TrimSuffix(issue.id, "-%d"),
				"compat:",
			),
			"-", " ",
		),
		p.Path,
	)

	// Check if we already have this notification.
	eventID := fmt.Sprintf(issue.id, profile.ID)
	n := notifications.Get(eventID)
	if n != nil {
		return
	}

	// Otherwise, create a new one.
	n = &notifications.Notification{
		EventID:      eventID,
		Type:         issue.level,
		Title:        fmt.Sprintf(issue.title, profile.Name),
		Message:      fmt.Sprintf(issue.message, profile.Name),
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
		func() {
			profile.Lock()
			defer profile.Unlock()

			profile.Warning = fmt.Sprintf(
				"%s  \nThis was last detected at %s.",
				fmt.Sprintf(issue.message, p.Name),
				time.Now().Format("15:04 on 2.1.2006"),
			)
			profile.WarningLastUpdated = time.Now()
		}()

		return profile.Save()
	})
}
