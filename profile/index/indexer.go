package index

import (
	"github.com/safing/portbase/database"
	"github.com/safing/portbase/database/query"
	"github.com/safing/portbase/database/record"
	"github.com/safing/portbase/log"
	"github.com/safing/portbase/modules"

	"github.com/safing/portmaster/profile"
)

// FIXME: listen for profile changes and update the index

var (
	indexDB = database.NewInterface(&database.Options{
		Local:                true, // we want to access crownjewel records
		AlwaysMakeCrownjewel: true, // never sync the index
	})
	indexSub *database.Subscription

	shutdownIndexer = make(chan struct{})
)

func init() {
	modules.Register("profile:index", nil, start, stop, "profile", "database")
}

func start() (err error) {
	indexSub, err = indexDB.Subscribe(query.New("core:profiles/user/"))
	if err != nil {
		return err
	}

	return nil
}

func stop() error {
	close(shutdownIndexer)
	indexSub.Cancel()
	return nil
}

func indexer() {
	for {
		select {
		case <-shutdownIndexer:
			return
		case r := <-indexSub.Feed:
			if r == nil {
				return
			}

			prof := ensureProfile(r)
			if prof != nil {
				for _, fp := range prof.Fingerprints {
					if fp.MatchesOS() && fp.Type == "full_path" {

						// get Profile and ensure identifier is set
						pi, err := Get("full_path", fp.Value)
						if err != nil {
							if err == database.ErrNotFound {
								pi = NewIndex(id)
							} else {
								log.Errorf("profile/index: could not save updated profile index: %s", err)
							}
						}

						if pi.AddUserProfile(prof.ID) {
							err := pi.Save()
							if err != nil {
								log.Errorf("profile/index: could not save updated profile index: %s", err)
							}
						}

					}
				}
			}
		}
	}
}

func ensureProfile(r record.Record) *profile.Profile {
	// unwrap
	if r.IsWrapped() {
		// only allocate a new struct, if we need it
		new := &profile.Profile{}
		err := record.Unwrap(r, new)
		if err != nil {
			log.Errorf("profile/index: could not unwrap Profile: %s", err)
			return nil
		}
		return new
	}

	// or adjust type
	new, ok := r.(*profile.Profile)
	if !ok {
		log.Errorf("profile/index: record not of type *Profile, but %T", r)
		return nil
	}
	return new
}
