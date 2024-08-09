package access

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/safing/portmaster/base/database"
	"github.com/safing/portmaster/base/database/query"
	"github.com/safing/portmaster/base/database/record"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/spn/access/token"
	"github.com/safing/structures/dsd"
)

func loadTokens() {
	for _, zone := range persistentZones {
		// Get handler of zone.
		handler, ok := token.GetHandler(zone)
		if !ok {
			log.Warningf("spn/access: could not find zone %s for loading tokens", zone)
			continue
		}

		// Get data from database.
		r, err := db.Get(fmt.Sprintf(tokenStorageKeyTemplate, zone))
		if err != nil {
			if errors.Is(err, database.ErrNotFound) {
				log.Debugf("spn/access: no %s tokens to load", zone)
			} else {
				log.Warningf("spn/access: failed to load %s tokens: %s", zone, err)
			}
			continue
		}

		// Get wrapper.
		wrapper, ok := r.(*record.Wrapper)
		if !ok {
			log.Warningf("spn/access: failed to parse %s tokens: expected wrapper, got %T", zone, r)
			continue
		}

		// Load into handler.
		err = handler.Load(wrapper.Data)
		if err != nil {
			log.Warningf("spn/access: failed to load %s tokens: %s", zone, err)
		}
		log.Infof("spn/access: loaded %d %s tokens", handler.Amount(), zone)
	}
}

func storeTokens() {
	for _, zone := range persistentZones {
		// Get handler of zone.
		handler, ok := token.GetHandler(zone)
		if !ok {
			log.Warningf("spn/access: could not find zone %s for storing tokens", zone)
			continue
		}

		// Generate storage key.
		storageKey := fmt.Sprintf(tokenStorageKeyTemplate, zone)

		// Check if there is data to save.
		amount := handler.Amount()
		if amount == 0 {
			// Remove possible old entry from database.
			err := db.Delete(storageKey)
			if err != nil {
				log.Warningf("spn/access: failed to delete possible old %s tokens from storage: %s", zone, err)
			}
			log.Debugf("spn/access: no %s tokens to store", zone)
			continue
		}

		// Export data.
		data, err := handler.Save()
		if err != nil {
			log.Warningf("spn/access: failed to export %s tokens for storing: %s", zone, err)
			continue
		}

		// Wrap data into raw record.
		r, err := record.NewWrapper(storageKey, nil, dsd.RAW, data)
		if err != nil {
			log.Warningf("spn/access: failed to prepare %s token export for storing: %s", zone, err)
			continue
		}

		// Let tokens expire after one month.
		// This will regularly happen when we switch zones.
		r.UpdateMeta()
		r.Meta().MakeSecret()
		r.Meta().MakeCrownJewel()
		r.Meta().SetRelativateExpiry(30 * 86400)

		// Save to database.
		err = db.Put(r)
		if err != nil {
			log.Warningf("spn/access: failed to store %s tokens: %s", zone, err)
			continue
		}

		log.Infof("spn/access: stored %d %s tokens", amount, zone)
	}
}

func clearTokens() {
	for _, zone := range persistentZones {
		// Get handler of zone.
		handler, ok := token.GetHandler(zone)
		if !ok {
			log.Warningf("spn/access: could not find zone %s for clearing tokens", zone)
			continue
		}

		// Clear tokens.
		handler.Clear()
	}

	// Purge database storage prefix.
	ctx, cancel := context.WithTimeout(module.mgr.Ctx(), 10*time.Second)
	defer cancel()
	n, err := db.Purge(ctx, query.New(fmt.Sprintf(tokenStorageKeyTemplate, "")))
	if err != nil {
		log.Warningf("spn/access: failed to clear token storages: %s", err)
		return
	}
	log.Infof("spn/access: cleared %d token storages", n)
}
