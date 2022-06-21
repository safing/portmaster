package broadcasts

import (
	"time"

	"github.com/safing/portbase/config"
	"github.com/safing/portmaster/intel/geoip"
	"github.com/safing/portmaster/netenv"
	"github.com/safing/portmaster/updates"
	"github.com/safing/spn/access"
	"github.com/safing/spn/captain"
)

var portmasterStarted = time.Now()

func collectData() interface{} {
	data := make(map[string]interface{})

	// Get data about versions.
	versions := updates.GetSimpleVersions()
	data["Updates"] = versions
	data["Version"] = versions.Build.Version
	numericVersion, err := MakeNumericVersion(versions.Build.Version)
	if err != nil {
		data["NumericVersion"] = &DataError{
			Error: err,
		}
	} else {
		data["NumericVersion"] = numericVersion
	}

	// Get data about install.
	installInfo, err := GetInstallInfo()
	if err != nil {
		data["Install"] = &DataError{
			Error: err,
		}
	} else {
		data["Install"] = installInfo
	}

	// Get global configuration.
	data["Config"] = config.GetActiveConfigValues()

	// Get data about device location.
	locs, ok := netenv.GetInternetLocation()
	if ok && locs.Best().LocationOrNil() != nil {
		loc := locs.Best()
		data["Location"] = &Location{
			Country:        loc.Location.Country.ISOCode,
			Coordinates:    loc.Location.Coordinates,
			ASN:            loc.Location.AutonomousSystemNumber,
			ASOrg:          loc.Location.AutonomousSystemOrganization,
			Source:         loc.Source,
			SourceAccuracy: loc.SourceAccuracy,
		}
	}

	// Get data about SPN status.
	data["SPN"] = captain.GetSPNStatus()

	// Get data about account.
	userRecord, err := access.GetUser()
	if err != nil {
		data["Account"] = &DataError{
			Error: err,
		}
	} else {
		data["Account"] = &Account{
			UserRecord: userRecord,
			UpToDate:   userRecord.Meta().Modified > time.Now().Add(-7*24*time.Hour).Unix(),
			MayUseUSP:  userRecord.MayUseSPN(),
		}
	}

	// Time running.
	data["UptimeHours"] = int(time.Since(portmasterStarted).Hours())

	return data
}

// Location holds location matching data.
type Location struct {
	Country        string
	Coordinates    geoip.Coordinates
	ASN            uint
	ASOrg          string
	Source         netenv.DeviceLocationSource
	SourceAccuracy int
}

// Account holds SPN account matching data.
type Account struct {
	*access.UserRecord
	UpToDate  bool
	MayUseUSP bool
}

// DataError represents an error getting some matching data.
type DataError struct {
	Error error
}
