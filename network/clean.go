package network

import (
	"context"
	"time"

	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/process"
)

var (
	cleanerTickDuration               = 10 * time.Second
	deleteLinksAfterEndedThreshold    = 5 * time.Minute
	deleteCommsWithoutLinksThreshhold = 3 * time.Minute

	mtSaveLink = "save network link"
)

func cleaner() {
	for {
		time.Sleep(cleanerTickDuration)

		activeComms := cleanLinks()
		activeProcs := cleanComms(activeComms)
		process.CleanProcessStorage(activeProcs)
	}
}

func cleanLinks() (activeComms map[string]struct{}) {
	activeComms = make(map[string]struct{})
	activeIDs := process.GetActiveConnectionIDs()

	now := time.Now().Unix()
	deleteOlderThan := time.Now().Add(-deleteLinksAfterEndedThreshold).Unix()

	linksLock.RLock()
	defer linksLock.RUnlock()

	var found bool
	for key, link := range links {

		// delete dead links
		link.Lock()
		deleteThis := link.Ended > 0 && link.Ended < deleteOlderThan
		link.Unlock()
		if deleteThis {
			log.Tracef("network.clean: deleted %s (ended at %d)", link.DatabaseKey(), link.Ended)
			go link.Delete()
			continue
		}

		// not yet deleted, so its still a valid link regarding link count
		comm := link.Communication()
		comm.Lock()
		markActive(activeComms, comm.DatabaseKey())
		comm.Unlock()

		// check if link is dead
		found = false
		for _, activeID := range activeIDs {
			if key == activeID {
				found = true
				break
			}
		}

		if !found {
			// mark end time
			link.Lock()
			link.Ended = now
			link.Unlock()
			log.Tracef("network.clean: marked %s as ended", link.DatabaseKey())
			// save
			linkToSave := link
			module.StartMicroTask(&mtSaveLink, func(ctx context.Context) error {
				linkToSave.saveAndLog()
				return nil
			})
		}

	}

	return activeComms
}

func cleanComms(activeLinks map[string]struct{}) (activeComms map[string]struct{}) {
	activeComms = make(map[string]struct{})

	commsLock.RLock()
	defer commsLock.RUnlock()

	threshold := time.Now().Add(-deleteCommsWithoutLinksThreshhold).Unix()
	for _, comm := range comms {
		// has links?
		_, hasLinks := activeLinks[comm.DatabaseKey()]

		// comm created
		comm.Lock()
		created := comm.Meta().Created
		comm.Unlock()

		if !hasLinks && created < threshold {
			log.Tracef("network.clean: deleted %s", comm.DatabaseKey())
			go comm.Delete()
		} else {
			p := comm.Process()
			p.Lock()
			markActive(activeComms, p.DatabaseKey())
			p.Unlock()
		}

	}
	return
}

func markActive(activeMap map[string]struct{}, key string) {
	_, ok := activeMap[key]
	if !ok {
		activeMap[key] = struct{}{}
	}
}
