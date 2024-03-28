package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"path"
	"strings"
	"time"

	diff "github.com/r3labs/diff/v3"
	"golang.org/x/exp/slices"

	"github.com/safing/portbase/database"
	"github.com/safing/portbase/database/query"
	"github.com/safing/portbase/log"
	"github.com/safing/portbase/modules"
	"github.com/safing/portmaster/spn/captain"
	"github.com/safing/portmaster/spn/navigator"
)

var (
	observerModule *modules.Module

	db = database.NewInterface(&database.Options{
		Local:    true,
		Internal: true,
	})

	reportAllChanges bool

	errNoChanges = errors.New("no changes")

	reportingDelayFlag string
	reportingDelay     = 10 * time.Minute
)

func init() {
	observerModule = modules.Register("observer", prepObserver, startObserver, nil, "captain", "apprise")

	flag.BoolVar(&reportAllChanges, "report-all-changes", false, "report all changes, no just interesting ones")
	flag.StringVar(&reportingDelayFlag, "reporting-delay", "10m", "delay reports to summarize changes")
}

func prepObserver() error {
	if reportingDelayFlag != "" {
		duration, err := time.ParseDuration(reportingDelayFlag)
		if err != nil {
			return fmt.Errorf("failed to parse reporting-delay: %w", err)
		}
		reportingDelay = duration
	}

	return nil
}

func startObserver() error {
	observerModule.StartServiceWorker("observer", 0, observerWorker)

	return nil
}

type observedPin struct {
	previous *navigator.PinExport
	latest   *navigator.PinExport

	lastUpdate         time.Time
	lastUpdateReported bool
}

type observedChange struct {
	Title   string
	Summary string

	UpdatedPin *navigator.PinExport
	UpdateTime time.Time

	SPNStatus *captain.SPNStatus
}

func observerWorker(ctx context.Context) error {
	log.Info("observer: starting")
	defer log.Info("observer: stopped")

	// Subscribe to SPN status.
	statusSub, err := db.Subscribe(query.New("runtime:spn/status"))
	if err != nil {
		return fmt.Errorf("failed to subscribe to spn status: %w", err)
	}
	defer statusSub.Cancel() //nolint:errcheck

	// Get latest status.
	latestStatus := captain.GetSPNStatus()

	// Step 1: Wait for SPN to connect, if needed.
	if latestStatus.Status != captain.StatusConnected {
		log.Info("observer: waiting for SPN to connect")
	waitForConnect:
		for {
			select {
			case r := <-statusSub.Feed:
				if r == nil {
					return errors.New("status feed ended")
				}

				statusUpdate, ok := r.(*captain.SPNStatus)
				switch {
				case !ok:
					log.Warningf("observer: received invalid SPN status: %s", r)
				case statusUpdate.Status == captain.StatusFailed:
					log.Warningf("observer: SPN failed to connect")
				case statusUpdate.Status == captain.StatusConnected:
					break waitForConnect
				}
			case <-ctx.Done():
				return nil
			}
		}
	}

	// Wait for one second for the navigator to settle things.
	log.Info("observer: connected to network, waiting for navigator")
	time.Sleep(1 * time.Second)

	// Step 2: Get current state.
	mapQuery := query.New("map:main/")
	q, err := db.Query(mapQuery)
	if err != nil {
		return fmt.Errorf("failed to start map query: %w", err)
	}
	defer q.Cancel()

	// Put all current pins in a map.
	observedPins := make(map[string]*observedPin)
query:
	for {
		select {
		case r := <-q.Next:
			// Check if we are done.
			if r == nil {
				break query
			}
			// Add all pins to seen pins.
			if pin, ok := r.(*navigator.PinExport); ok {
				observedPins[pin.ID] = &observedPin{
					previous: pin,
					latest:   pin,
				}
			} else {
				log.Warningf("observer: received invalid pin export: %s", r)
			}
		case <-ctx.Done():
			return nil
		}
	}
	if q.Err() != nil {
		return fmt.Errorf("failed to finish map query: %w", q.Err())
	}

	// Step 3: Monitor for changes.
	sub, err := db.Subscribe(mapQuery)
	if err != nil {
		return fmt.Errorf("failed to start map sub: %w", err)
	}
	defer sub.Cancel() //nolint:errcheck

	// Start ticker for checking for changes.
	reportChangesTicker := time.NewTicker(10 * time.Second)
	defer reportChangesTicker.Stop()

	log.Info("observer: listening for hub changes")
	for {
		select {
		case <-ctx.Done():
			return nil

		case r := <-statusSub.Feed:
			// Keep SPN connection status up to date.
			if r == nil {
				return errors.New("status feed ended")
			}
			if statusUpdate, ok := r.(*captain.SPNStatus); ok {
				latestStatus = statusUpdate
				log.Infof("observer: SPN status is now %s", statusUpdate.Status)
			} else {
				log.Warningf("observer: received invalid pin export: %s", r)
			}

		case r := <-sub.Feed:
			// Save all observed pins.
			switch {
			case r == nil:
				return errors.New("pin feed ended")
			case r.Meta().IsDeleted():
				delete(observedPins, path.Base(r.DatabaseKey()))
			default:
				if pin, ok := r.(*navigator.PinExport); ok {
					existingObservedPin, ok := observedPins[pin.ID]
					if ok {
						// Update previously observed Hub.
						existingObservedPin.latest = pin
						existingObservedPin.lastUpdate = time.Now()
						existingObservedPin.lastUpdateReported = false
					} else {
						// Add new Hub.
						observedPins[pin.ID] = &observedPin{
							latest:     pin,
							lastUpdate: time.Now(),
						}
					}
				} else {
					log.Warningf("observer: received invalid pin export: %s", r)
				}
			}

		case <-reportChangesTicker.C:
			// Report changed pins.

			for _, observedPin := range observedPins {
				// Check if context was canceled.
				select {
				case <-ctx.Done():
					return nil
				default:
				}

				switch {
				case observedPin.lastUpdateReported:
					// Change already reported.
				case time.Since(observedPin.lastUpdate) < reportingDelay:
					// Only report changes if older than the configured delay.
				default:
					// Format and report.
					title, changes, err := formatPinChanges(observedPin.previous, observedPin.latest)
					if err != nil {
						if !errors.Is(err, errNoChanges) {
							log.Warningf("observer: failed to format pin changes: %s", err)
						}
					} else {
						// Report changes.
						reportChanges(&observedChange{
							Title:      title,
							Summary:    changes,
							UpdatedPin: observedPin.latest,
							UpdateTime: observedPin.lastUpdate,
							SPNStatus:  latestStatus,
						})
					}

					// Update observed pin.
					observedPin.previous = observedPin.latest
					observedPin.lastUpdateReported = true
				}
			}
		}
	}
}

func reportChanges(change *observedChange) {
	// Log changes.
	log.Infof("observer:\n%s\n%s", change.Title, change.Summary)

	// Report via Apprise.
	err := reportToApprise(change)
	if err != nil {
		log.Warningf("observer: failed to report changes to apprise: %s", err)
	}
}

var (
	ignoreChangesIn = []string{
		"ConnectedTo",
		"HopDistance",
		"Info.entryPolicy", // Alternatively, ignore "Info.Entry"
		"Info.exitPolicy",  // Alternatively, ignore "Info.Exit"
		"Info.parsedTransports",
		"Info.Timestamp",
		"SessionActive",
		"Status.Keys",
		"Status.Lanes",
		"Status.Load",
		"Status.Timestamp",
	}

	ignoreStates = []string{
		"IsHomeHub",
		"Failing",
	}
)

func ignoreChange(path string) bool {
	if reportAllChanges {
		return false
	}

	for _, pathPrefix := range ignoreChangesIn {
		if strings.HasPrefix(path, pathPrefix) {
			return true
		}
	}
	return false
}

func formatPinChanges(from, to *navigator.PinExport) (title, changes string, err error) {
	// Return immediately if pin is new.
	if from == nil {
		return fmt.Sprintf("New Hub: %s", makeHubName(to.Name, to.ID)), "", nil
	}

	// Find notable changes.
	changelog, err := diff.Diff(from, to)
	if err != nil {
		return "", "", fmt.Errorf("failed to diff: %w", err)
	}
	if len(changelog) > 0 {
		// Build changelog message.
		changes := make([]string, 0, len(changelog))
		for _, change := range changelog {
			// Create path to changed field.
			fullPath := strings.Join(change.Path, ".")

			// Check if this path should be ignored.
			if ignoreChange(fullPath) {
				continue
			}

			// Add to reportable changes.
			changeMsg := formatChange(change, fullPath)
			if changeMsg != "" {
				changes = append(changes, changeMsg)
			}
		}

		// Log the changes, if there are any left.
		if len(changes) > 0 {
			return fmt.Sprintf("Hub Changed: %s", makeHubName(to.Name, to.ID)),
				strings.Join(changes, "\n"),
				nil
		}
	}

	return "", "", errNoChanges
}

func formatChange(change diff.Change, fullPath string) string {
	switch {
	case strings.HasPrefix(fullPath, "States"):
		switch change.Type {
		case diff.CREATE:
			return formatState(fmt.Sprintf("%v", change.To), true)
		case diff.UPDATE:
			a := formatState(fmt.Sprintf("%v", change.To), true)
			b := formatState(fmt.Sprintf("%v", change.From), false)
			switch {
			case a != "" && b != "":
				return a + "\n" + b
			case a != "":
				return a
			case b != "":
				return b
			}
		case diff.DELETE:
			return formatState(fmt.Sprintf("%v", change.From), false)
		}

	default:
		switch change.Type {
		case diff.CREATE:
			return fmt.Sprintf("%s added %v", fullPath, change.To)
		case diff.UPDATE:
			return fmt.Sprintf("%s changed from %v to %v", fullPath, change.From, change.To)
		case diff.DELETE:
			return fmt.Sprintf("%s removed %v", fullPath, change.From)
		}
	}

	return ""
}

func formatState(name string, isSet bool) string {
	// Check if state should be ignored.
	if !reportAllChanges && slices.Contains[[]string, string](ignoreStates, name) {
		return ""
	}

	if isSet {
		return fmt.Sprintf("State is %v", name)
	}
	return fmt.Sprintf("State is NOT %v", name)
}

func makeHubName(name, id string) string {
	shortenedID := id[len(id)-8:len(id)-4] +
		"-" +
		id[len(id)-4:]

	// Be more careful, as the Hub name is user input.
	switch {
	case name == "":
		return shortenedID
	case len(name) > 16:
		return fmt.Sprintf("%s (%s)", name[:16], shortenedID)
	default:
		return fmt.Sprintf("%s (%s)", name, shortenedID)
	}
}
