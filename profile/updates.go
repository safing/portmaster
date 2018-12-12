package profile

import (
	"strings"

	"github.com/Safing/portbase/database"
	"github.com/Safing/portbase/database/query"
	"github.com/Safing/portbase/log"
)

func initUpdateListener() error {
	sub, err := profileDB.Subscribe(query.New(MakeProfileKey(SpecialNamespace, "")))
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
				log.Errorf("profile: received update for special profile, but could not read: %s", err)
				continue
			}

			switch profile.ID {
			case "global":
				specialProfileLock.Lock()
				globalProfile = profile
				specialProfileLock.Unlock()
			case "fallback":
				specialProfileLock.Lock()
				fallbackProfile = profile
				specialProfileLock.Unlock()
			default:
				switch {
				case strings.HasPrefix(profile.Key(), MakeProfileKey(UserNamespace, "")):
					updateActiveUserProfile(profile)
				case strings.HasPrefix(profile.Key(), MakeProfileKey(StampNamespace, "")):
					updateActiveStampProfile(profile)
				}
			}

		}
	}
}
