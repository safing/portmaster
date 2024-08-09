package broadcasts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ghodss/yaml"

	"github.com/safing/portmaster/base/database"
	"github.com/safing/portmaster/base/database/accessor"
	"github.com/safing/portmaster/base/database/query"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/notifications"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/updates"
)

const (
	broadcastsResourcePath = "intel/portmaster/notifications.yaml"

	broadcastNotificationIDPrefix = "broadcasts:"

	minRepeatDuration = 1 * time.Hour
)

// Errors.
var (
	ErrSkip                  = errors.New("broadcast skipped")
	ErrSkipDoesNotMatch      = fmt.Errorf("%w: does not match", ErrSkip)
	ErrSkipAlreadyActive     = fmt.Errorf("%w: already active", ErrSkip)
	ErrSkipAlreadyShown      = fmt.Errorf("%w: already shown", ErrSkip)
	ErrSkipRemovedByMismatch = fmt.Errorf("%w: removed due to mismatch", ErrSkip)
	ErrSkipRemovedBySource   = fmt.Errorf("%w: removed by source", ErrSkip)
)

// BroadcastNotifications holds the data structure of the broadcast
// notifications update file.
type BroadcastNotifications struct {
	Notifications map[string]*BroadcastNotification
}

// BroadcastNotification is a single broadcast notification.
type BroadcastNotification struct {
	*notifications.Notification
	id string

	// Match holds a query string that needs to match the local matching data in
	// order for the broadcast to be displayed.
	Match         string
	matchingQuery *query.Query
	// AttachToModule signifies if the broadcast notification should be attached to the module.
	AttachToModule bool
	// Remove signifies that the broadcast should be canceled and its state removed.
	Remove bool
	// Permanent signifies that the broadcast cannot be acknowledge by the user
	// and remains in the UI indefinitely.
	Permanent bool
	// Repeat specifies a duration after which the broadcast should be shown again.
	Repeat         string
	repeatDuration time.Duration
}

func broadcastNotify(ctx *mgr.WorkerCtx) error {
	// Get broadcast notifications file, load it from disk and parse it.
	broadcastsResource, err := updates.GetFile(broadcastsResourcePath)
	if err != nil {
		return fmt.Errorf("failed to get broadcast notifications update: %w", err)
	}
	broadcastsData, err := os.ReadFile(broadcastsResource.Path())
	if err != nil {
		return fmt.Errorf("failed to load broadcast notifications update: %w", err)
	}
	broadcasts, err := parseBroadcastSource(broadcastsData)
	if err != nil {
		return fmt.Errorf("failed to parse broadcast notifications update: %w", err)
	}

	// Get and marshal matching data.
	matchingData := collectData()
	matchingJSON, err := json.Marshal(matchingData)
	if err != nil {
		return fmt.Errorf("failed to marshal broadcast notifications matching data: %w", err)
	}
	matchingDataAccessor := accessor.NewJSONBytesAccessor(&matchingJSON)

	// Get broadcast notification states.
	bss, err := getBroadcastStates()
	if err != nil {
		if !errors.Is(err, database.ErrNotFound) {
			return fmt.Errorf("failed to get broadcast notifications states: %w", err)
		}
		bss = newBroadcastStates()
	}

	// Go through all broadcast nofications and check if they match.
	for _, bn := range broadcasts.Notifications {
		err := handleBroadcast(bn, matchingDataAccessor, bss)
		switch {
		case err == nil:
			log.Infof("broadcasts: displaying broadcast %s", bn.id)
		case errors.Is(err, ErrSkip):
			log.Tracef("broadcasts: skipped displaying broadcast %s: %s", bn.id, err)
		default:
			log.Warningf("broadcasts: failed to handle broadcast %s: %s", bn.id, err)
		}
	}

	return nil
}

func parseBroadcastSource(yamlData []byte) (*BroadcastNotifications, error) {
	// Parse data.
	broadcasts := &BroadcastNotifications{}
	err := yaml.Unmarshal(yamlData, broadcasts)
	if err != nil {
		return nil, err
	}

	// Add IDs to struct for easier handling.
	for id, bn := range broadcasts.Notifications {
		bn.id = id

		// Parse matching query.
		if bn.Match != "" {
			q, err := query.ParseQuery("query / where " + bn.Match)
			if err != nil {
				return nil, fmt.Errorf("failed to parse query of broadcast notification %s: %w", bn.id, err)
			}
			bn.matchingQuery = q
		}

		// Parse the repeat duration.
		if bn.Repeat != "" {
			duration, err := time.ParseDuration(bn.Repeat)
			if err != nil {
				return nil, fmt.Errorf("failed to parse repeat duration of broadcast notification %s: %w", bn.id, err)
			}
			bn.repeatDuration = duration
			// Raise duration to minimum.
			if bn.repeatDuration < minRepeatDuration {
				bn.repeatDuration = minRepeatDuration
			}
		}
	}

	return broadcasts, nil
}

func handleBroadcast(bn *BroadcastNotification, matchingDataAccessor accessor.Accessor, bss *BroadcastStates) error {
	// Check if broadcast was already shown.
	if bss != nil {
		state, ok := bss.States[bn.id]
		switch {
		case !ok || state.Read.IsZero():
			// Was never shown, continue.
		case bn.repeatDuration == 0:
			// Was already shown and is not repeated, skip.
			return ErrSkipAlreadyShown
		case time.Now().Before(state.Read.Add(bn.repeatDuration)):
			// Was already shown and should be repeated - but not yet, skip.
			return ErrSkipAlreadyShown
		}
	}

	// Check if broadcast should be removed.
	if bn.Remove {
		removeBroadcast(bn, bss)
		return ErrSkipRemovedBySource
	}

	// Skip if broadcast does not match.
	if bn.matchingQuery != nil && !bn.matchingQuery.MatchesAccessor(matchingDataAccessor) {
		removed := removeBroadcast(bn, bss)
		if removed {
			return ErrSkipRemovedByMismatch
		}
		return ErrSkipDoesNotMatch
	}

	// Check if there is already an active notification for this.
	eventID := broadcastNotificationIDPrefix + bn.id
	n := notifications.Get(eventID)
	if n != nil {
		// Already active!
		return ErrSkipAlreadyActive
	}

	// Prepare notification for displaying.
	n = bn.Notification
	n.EventID = eventID
	n.GUID = ""
	n.State = ""
	n.SelectedActionID = ""

	// It is okay to edit the notification, as they are loaded from the file every time.
	// Add dismiss button if the notification is not permanent.
	if !bn.Permanent {
		n.AvailableActions = append(n.AvailableActions, &notifications.Action{
			ID:   "ack",
			Text: "Got it!",
		})
	}
	n.SetActionFunction(markBroadcastAsRead)

	// Display notification.
	n.Save()
	if bn.AttachToModule {
		n.SyncWithState(module.states)
	}

	return nil
}

func removeBroadcast(bn *BroadcastNotification, bss *BroadcastStates) (removed bool) {
	// Remove any active notification.
	n := notifications.Get(broadcastNotificationIDPrefix + bn.id)
	if n != nil {
		removed = true
		n.Delete()
	}

	// Remove any state.
	if bss != nil {
		delete(bss.States, bn.id)
	}

	return
}

var savingBroadcastStateLock sync.Mutex

func markBroadcastAsRead(ctx context.Context, n *notifications.Notification) error {
	// Lock persisting broadcast state.
	savingBroadcastStateLock.Lock()
	defer savingBroadcastStateLock.Unlock()

	// Get notification data.
	var broadcastID, actionID string
	func() {
		n.Lock()
		defer n.Unlock()
		broadcastID = strings.TrimPrefix(n.EventID, broadcastNotificationIDPrefix)
		actionID = n.SelectedActionID
	}()

	// Check response.
	switch actionID {
	case "ack":
	case "":
		return fmt.Errorf("no action ID for %s", broadcastID)
	default:
		return fmt.Errorf("unexpected action ID for %s: %s", broadcastID, actionID)
	}

	// Get broadcast notification states.
	bss, err := getBroadcastStates()
	if err != nil {
		if !errors.Is(err, database.ErrNotFound) {
			return fmt.Errorf("failed to get broadcast notifications states: %w", err)
		}
		bss = newBroadcastStates()
	}

	// Get state for this notification.
	bs, ok := bss.States[broadcastID]
	if !ok {
		bs = &BroadcastState{}
		bss.States[broadcastID] = bs
	}

	// Delete to allow for timely repeats.
	n.Delete()

	// Mark as read and save to DB.
	log.Infof("broadcasts: user acknowledged broadcast %s", broadcastID)
	bs.Read = time.Now()
	return bss.save()
}
