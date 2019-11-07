package profile

import (
	"strings"
	"sync/atomic"

	"github.com/safing/portbase/database"
	"github.com/safing/portbase/database/query"
	"github.com/safing/portbase/log"
)

func initUpdateListener() error {
	sub, err := profileDB.Subscribe(query.New("core:profiles/"))
	if err != nil {
		return err
	}

	go updateListener(sub)
	return nil
}

func updateListener(sub *database.Subscription) {
	for {
		select {
		case <-shutdownSignal:
			return
		case r := <-sub.Feed:

			if r.Meta().IsDeleted() {
				continue
			}

			profile, err := EnsureProfile(r)
			if err != nil {
				log.Errorf("profile: received update for profile, but could not read: %s", err)
				continue
			}

			log.Infof("profile: updated %s", profile.ID)

			switch profile.DatabaseKey() {
			case "profiles/special/global":

				specialProfileLock.Lock()
				globalProfile = profile
				specialProfileLock.Unlock()

			case "profiles/special/fallback":

				profile.Lock()
				profileChanged := ensureServiceEndpointsDenyAll(profile)
				profile.Unlock()

				if profileChanged {
					_ = profile.Save(SpecialNamespace)
					continue
				}

				specialProfileLock.Lock()
				fallbackProfile = profile
				specialProfileLock.Unlock()

			default:

				switch {
				case strings.HasPrefix(profile.Key(), MakeProfileKey(UserNamespace, "")):
					updateActiveProfile(profile, true /* User Profile */)
				case strings.HasPrefix(profile.Key(), MakeProfileKey(StampNamespace, "")):
					updateActiveProfile(profile, false /* Stamp Profile */)
				}

			}
		}
	}
}

var (
	updateVersion uint32
)

// GetUpdateVersion returns the current profiles internal update version
func GetUpdateVersion() uint32 {
	return atomic.LoadUint32(&updateVersion)
}

func increaseUpdateVersion() {
	// we intentially want to wrap
	atomic.AddUint32(&updateVersion, 1)
}
