package broadcasts

import (
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	semver "github.com/hashicorp/go-version"

	"github.com/safing/portmaster/base/database"
	"github.com/safing/portmaster/base/database/query"
	"github.com/safing/portmaster/base/database/record"
	"github.com/safing/portmaster/base/info"
	"github.com/safing/portmaster/base/log"
)

const installInfoDBKey = "core:status/install-info"

// InstallInfo holds generic info about the install.
type InstallInfo struct {
	record.Base
	sync.Mutex

	Version        string
	NumericVersion int64

	Time             time.Time
	NumericDate      int64
	DaysSinceInstall int64
	UnixTimestamp    int64
}

// GetInstallInfo returns the install info from the database.
func GetInstallInfo() (*InstallInfo, error) {
	r, err := db.Get(installInfoDBKey)
	if err != nil {
		return nil, err
	}

	// Unwrap.
	if r.IsWrapped() {
		// Only allocate a new struct, if we need it.
		newRecord := &InstallInfo{}
		err = record.Unwrap(r, newRecord)
		if err != nil {
			return nil, err
		}
		return newRecord, nil
	}

	// or adjust type
	newRecord, ok := r.(*InstallInfo)
	if !ok {
		return nil, fmt.Errorf("record not of type *InstallInfo, but %T", r)
	}
	return newRecord, nil
}

func ensureInstallInfo() {
	// Get current install info from database.
	installInfo, err := GetInstallInfo()
	if err != nil {
		installInfo = &InstallInfo{}
		if !errors.Is(err, database.ErrNotFound) {
			log.Warningf("updates: failed to load install info: %s", err)
		}
	}

	// Fill in missing data and save.
	installInfo.checkAll()
	if err := installInfo.save(); err != nil {
		log.Warningf("updates: failed to save install info: %s", err)
	}
}

func (ii *InstallInfo) save() error {
	if !ii.KeyIsSet() {
		ii.SetKey(installInfoDBKey)
	}
	return db.Put(ii)
}

func (ii *InstallInfo) checkAll() {
	ii.checkVersion()
	ii.checkInstallDate()
}

func (ii *InstallInfo) checkVersion() {
	// Check if everything is present.
	if ii.Version != "" && ii.NumericVersion > 0 {
		return
	}

	// Update version information.
	versionInfo := info.GetInfo()
	ii.Version = versionInfo.Version

	// Update numeric version.
	if versionInfo.Version != "" {
		numericVersion, err := MakeNumericVersion(versionInfo.Version)
		if err != nil {
			log.Warningf("updates: failed to make numeric version: %s", err)
		} else {
			ii.NumericVersion = numericVersion
		}
	}
}

// MakeNumericVersion makes a numeric version with the first three version
// segment always using three digits.
func MakeNumericVersion(version string) (numericVersion int64, err error) {
	// Parse version string.
	ver, err := semver.NewVersion(version)
	if err != nil {
		return 0, fmt.Errorf("failed to parse core version: %w", err)
	}

	// Transform version for numeric representation.
	segments := ver.Segments()
	for i := 0; i < 3 && i < len(segments); i++ {
		segmentNumber := int64(segments[i])
		if segmentNumber > 999 {
			segmentNumber = 999
		}
		switch i {
		case 0:
			numericVersion += segmentNumber * 1000000
		case 1:
			numericVersion += segmentNumber * 1000
		case 2:
			numericVersion += segmentNumber
		}
	}

	return numericVersion, nil
}

func (ii *InstallInfo) checkInstallDate() {
	// Check if everything is present.
	if ii.UnixTimestamp > 0 &&
		ii.NumericDate > 0 &&
		ii.DaysSinceInstall > 0 &&
		!ii.Time.IsZero() {
		return
	}

	// Find oldest created database entry and use it as install time.
	oldest := time.Now().Unix()
	it, err := db.Query(query.New("core"))
	if err != nil {
		log.Warningf("updates: failed to create iterator for searching DB for install time: %s", err)
		return
	}
	defer it.Cancel()
	for r := range it.Next {
		if oldest > r.Meta().Created {
			oldest = r.Meta().Created
		}
	}

	// Set data.
	ii.UnixTimestamp = oldest
	ii.Time = time.Unix(oldest, 0)
	ii.DaysSinceInstall = int64(time.Since(ii.Time).Hours()) / 24

	// Transform date for numeric representation.
	numericDate, err := strconv.ParseInt(ii.Time.Format("20060102"), 10, 64)
	if err != nil {
		log.Warningf("updates: failed to make numeric date from %s: %s", ii.Time, err)
	} else {
		ii.NumericDate = numericDate
	}
}
