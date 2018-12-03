package profile

import (
	"fmt"
	"strings"

	"github.com/Safing/portbase/database"
	"github.com/Safing/portbase/database/query"
	"github.com/Safing/portbase/log"
)

func initUpdateListener() error {
	sub, err := profileDB.Subscribe(query.New(makeProfileKey(specialNamespace, "")))
	if err != nil {
		return err
	}

	go updateListener(sub)
	return nil
}

var (
	slashedUserNamespace  = fmt.Sprintf("/%s/", userNamespace)
	slashedStampNamespace = fmt.Sprintf("/%s/", stampNamespace)
)

func updateListener(sub *database.Subscription) {
	for r := range sub.Feed {
		profile, err := ensureProfile(r)
		if err != nil {
			log.Errorf("profile: received update for special profile, but could not read: %s", err)
			continue
		}

		specialProfileLock.Lock()
		switch profile.ID {
		case "global":
			globalProfile = profile
			updateActiveGlobalProfile(profile)
		case "fallback":
			fallbackProfile = profile
			updateActiveFallbackProfile(profile)
		default:
			switch {
			case strings.HasPrefix(profile.Key(), makeProfileKey(userNamespace, "")):
				updateActiveUserProfile(profile)
			case strings.HasPrefix(profile.Key(), makeProfileKey(stampNamespace, "")):
				updateActiveStampProfile(profile)
			}
		}
		specialProfileLock.Unlock()
	}
}
