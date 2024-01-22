//go:build windows
// +build windows

package dnsevtlog

// This code is copied from Promtail v1.6.2-0.20231004111112-07cbef92268a with minor changes.

import (
	"fmt"
	"sync"
	"time"

	"golang.org/x/sys/windows"

	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/systemdns/dnsevtlog/win_eventlog"
)

type Subscription struct {
	subscription win_eventlog.EvtHandle
	fetcher      *win_eventlog.EventFetcher

	eventLogName  string
	eventLogQuery string

	ready bool
	done  chan struct{}
	wg    sync.WaitGroup
	err   error
}

// NewSubscription create a new windows event subscriptions.
func NewSubscription() (*Subscription, error) {
	sigEvent, err := windows.CreateEvent(nil, 0, 0, nil)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(sigEvent)

	t := &Subscription{
		eventLogName:  "Microsoft-Windows-DNS-Client/Operational",
		eventLogQuery: "*",
		done:          make(chan struct{}),
		fetcher:       win_eventlog.NewEventFetcher(),
	}

	subsHandle, err := win_eventlog.EvtSubscribe(t.eventLogName, t.eventLogQuery)
	if err != nil {
		return nil, fmt.Errorf("error subscribing to windows events: %w", err)
	}
	t.subscription = subsHandle

	return t, nil
}

// loop fetches new events and send them to via the Loki client.
func (t *Subscription) ReadWorker() {
	t.ready = true
	t.wg.Add(1)
	interval := time.NewTicker(time.Second)
	defer func() {
		t.ready = false
		t.wg.Done()
		interval.Stop()
	}()

	for {

	loop:
		for {
			// fetch events until there's no more.
			events, handles, err := t.fetcher.FetchEvents(t.subscription, 1033) // 1033: English
			if err != nil {
				if err != win_eventlog.ERROR_NO_MORE_ITEMS {
					t.err = err
					log.Warningf("dns event log: failed to fetch events: %s", err)
				} else {
					log.Debug("dns event log: no more entries")
				}
				break loop
			}
			t.err = nil
			// we have received events to handle.
			for _, entry := range events {
				log.Debugf("dns event log: %+v", entry)
			}
			win_eventlog.Close(handles)
		}
		// no more messages we wait for next poll timer tick.
		select {
		case <-t.done:
			return
		case <-interval.C:
		}
	}
}

// renderEntries renders Loki entries from windows event logs
// func (t *Subscription) renderEntries(events []win_eventlog.Event) []api.Entry {
// 	res := make([]api.Entry, 0, len(events))
// 	lbs := labels.NewBuilder(nil)
// 	for _, event := range events {
// 		entry := api.Entry{
// 			Labels: make(model.LabelSet),
// 		}

// 		entry.Timestamp = time.Now()
// 		if t.cfg.UseIncomingTimestamp {
// 			timeStamp, err := time.Parse(time.RFC3339Nano, fmt.Sprintf("%v", event.TimeCreated.SystemTime))
// 			if err != nil {
// 				level.Warn(t.logger).Log("msg", "error parsing timestamp", "err", err)
// 			} else {
// 				entry.Timestamp = timeStamp
// 			}
// 		}

// 		for _, lbl := range processed {
// 			if strings.HasPrefix(lbl.Name, "__") {
// 				continue
// 			}
// 			entry.Labels[model.LabelName(lbl.Name)] = model.LabelValue(lbl.Value)
// 		}

// 		line, err := formatLine(t.cfg, event)
// 		if err != nil {
// 			level.Warn(t.logger).Log("msg", "error formatting event", "err", err)
// 			continue
// 		}
// 		entry.Line = line
// 		res = append(res, entry)
// 	}
// 	return res
// }

func (t *Subscription) Stop() error {
	close(t.done)
	t.wg.Wait()
	return t.err
}
