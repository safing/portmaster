// Copyright Safing ICS Technologies GmbH. Use of this source code is governed by the AGPL license that can be found in the LICENSE file.

package profiles

import (
	"runtime"

	ds "github.com/ipfs/go-datastore"

	"github.com/Safing/safing-core/database"
	"github.com/Safing/safing-core/log"
)

func init() {

	// Data here is for demo purposes, Profiles will be served over network soon™.

	log.Tracef("profiles: loading sample profiles for %s", runtime.GOOS)

	switch runtime.GOOS {
	case "linux":

		log.Trace("profiles: loading linux sample profiles")

		(&Profile{
			Name:         "Chromium",
			Description:  "Browser by Google",
			Path:         "/usr/lib/chromium-browser/chromium-browser",
			Flags:        []int8{User, Internet, LocalNet, Browser},
			ConnectPorts: []uint16{80, 443},
		}).CreateInDist()

		(&Profile{
			Name:          "Evolution",
			Description:   "PIM solution by GNOME",
			Path:          "/usr/bin/evolution",
			Flags:         []int8{User, Internet, Gateway},
			ConnectPorts:  []uint16{25, 80, 143, 443, 465, 587, 993, 995},
			SecurityLevel: 2,
		}).CreateInDist()

		(&Profile{
			Name:          "Evolution Calendar",
			Description:   "PIM solution by GNOME - Calendar",
			Path:          "/usr/lib/evolution/evolution-calendar-factory-subprocess",
			Flags:         []int8{User, Internet, Gateway},
			ConnectPorts:  []uint16{80, 443},
			SecurityLevel: 2,
		}).CreateInDist()

		(&Profile{
			Name:         "Spotify",
			Description:  "Music streaming",
			Path:         "/usr/share/spotify/spotify",
			ConnectPorts: []uint16{80, 443, 4070},
			Flags:        []int8{User, Internet, Strict},
		}).CreateInDist()

		(&Profile{
			// flatpak edition
			Name:         "Spotify",
			Description:  "Music streaming",
			Path:         "/newroot/app/extra/share/spotify/spotify",
			ConnectPorts: []uint16{80, 443, 4070},
			Flags:        []int8{User, Internet, Strict},
		}).CreateInDist()

		(&Profile{
			Name:          "Evince",
			Description:   "PDF Document Reader",
			Path:          "/usr/bin/evince",
			Flags:         []int8{},
			SecurityLevel: 2,
		}).CreateInDist()

		(&Profile{
			Name:        "Ahavi",
			Description: "mDNS service",
			Path:        "/usr/bin/avahi-daemon",
			Flags:       []int8{System, LocalNet, Service, Directconnect},
		}).CreateInDist()

		(&Profile{
			Name:        "Python 2.7 Framework",
			Description: "Correctly handle python scripts",
			Path:        "/usr/bin/python2.7",
			Framework: &Framework{
				Find:  "^[^ ]+ ([^ ]+)",
				Build: "{1}|{CWD}/{1}",
			},
		}).CreateInDist()

		(&Profile{
			Name:        "Python 3.5 Framework",
			Description: "Correctly handle python scripts",
			Path:        "/usr/bin/python3.5",
			Framework: &Framework{
				Find:  "^[^ ]+ ([^ ]+)",
				Build: "{1}|{CWD}/{1}",
			},
		}).CreateInDist()

		(&Profile{
			Name:        "DHCP Client",
			Description: "Client software for the DHCP protocol",
			Path:        "/sbin/dhclient",
			Framework: &Framework{
				FindParent:      1,
				MergeWithParent: true,
			},
		}).CreateInDist()

		// Default Profiles
		// Until Profiles are distributed over the network, default profiles are activated when the Default Profile for "/" is missing.

		if ok, err := database.Has(ds.NewKey("/Data/Profiles/Profile_d-2f")); !ok || err != nil {

			log.Trace("profiles: loading linux default sample profiles")

			(&Profile{
				Name:        "Default Base",
				Description: "Default Profile for /",
				Path:        "/",
				Flags:       []int8{Internet, LocalNet, Strict},
				Default:     true,
			}).Create()

			(&Profile{
				Name:        "Installed Applications",
				Description: "Default Profile for /usr/bin",
				Path:        "/usr/bin/",
				Flags:       []int8{Internet, LocalNet, Gateway},
				Default:     true,
			}).Create()

			(&Profile{
				Name:        "System Binaries (/sbin)",
				Description: "Default Profile for ~/Downloads",
				Path:        "/sbin/",
				Flags:       []int8{Internet, LocalNet, Directconnect, Service, System},
				Default:     true,
			}).Create()

			(&Profile{
				Name:        "System Binaries (/usr/sbin)",
				Description: "Default Profile for ~/Downloads",
				Path:        "/usr/sbin/",
				Flags:       []int8{Internet, LocalNet, Directconnect, Service, System},
				Default:     true,
			}).Create()

			(&Profile{
				Name:        "System Tmp folder",
				Description: "Default Profile for /tmp",
				Path:        "/tmp/",
				Flags:       []int8{}, // deny all
				Default:     true,
			}).Create()

			(&Profile{
				Name:        "User Home",
				Description: "Default Profile for ~/",
				Path:        "~/",
				Flags:       []int8{Internet, LocalNet, Gateway},
				Default:     true,
			}).Create()

			(&Profile{
				Name:        "User Downloads",
				Description: "Default Profile for ~/Downloads",
				Path:        "~/Downloads/",
				Flags:       []int8{}, // deny all
				Default:     true,
			}).Create()

			(&Profile{
				Name:        "User Cache",
				Description: "Default Profile for ~/.cache",
				Path:        "~/.cache/",
				Flags:       []int8{}, // deny all
				Default:     true,
			}).Create()

		}

	case "windows":

		log.Trace("profiles: loading windows sample profiles")

		(&Profile{
			Name:         "Firefox",
			Description:  "Firefox Browser by Mozilla",
			Path:         "C:\\Program Files\\Mozilla Firefox\\firefox.exe",
			Flags:        []int8{User, Internet, LocalNet, Browser},
			ConnectPorts: []uint16{80, 443},
		}).CreateInDist()

		// Default Profiles
		// Until Profiles are distributed over the network, default profiles are activated when the Default Profile for "C" is missing.

		if ok, err := database.Has(ds.NewKey("/Data/Profiles/Profile:d-C")); !ok || err != nil {

			log.Trace("profiles: loading windows default sample profiles")

			(&Profile{
				Name:        "Default Base",
				Description: "Default Profile for C",
				Path:        "C",
				Flags:       []int8{Internet, LocalNet, Strict},
				Default:     true,
			}).Create()

			(&Profile{
				Name:        "Installed Applications",
				Description: "Default Profile for C:\\Program Files",
				Path:        "C:\\Program Files\\",
				Flags:       []int8{Internet, LocalNet, Gateway},
				Default:     true,
			}).Create()

			(&Profile{
				Name:        "Installed Applications (x86)",
				Description: "Default Profile for C:\\Program Files (x86)",
				Path:        "C:\\Program Files (x86)\\",
				Flags:       []int8{Internet, LocalNet, Gateway},
				Default:     true,
			}).Create()

			(&Profile{
				Name:        "System Applications (C:\\Windows\\System32)",
				Description: "Default Profile for C:\\Windows\\System32",
				Path:        "C:\\Windows\\System32\\",
				Flags:       []int8{Internet, LocalNet, Directconnect, Service, System},
				Default:     true,
			}).Create()

			(&Profile{
				Name:        "User Home",
				Description: "Default Profile for ~/",
				Path:        "~/",
				Flags:       []int8{Internet, LocalNet, Gateway},
				Default:     true,
			}).Create()

			(&Profile{
				Name:        "User Downloads",
				Description: "Default Profile for ~/Downloads",
				Path:        "~/Downloads/",
				Flags:       []int8{}, // deny all
				Default:     true,
			}).Create()

			(&Profile{
				Name:        "User Cache",
				Description: "Default Profile for ~/.cache",
				Path:        "~/.cache/",
				Flags:       []int8{}, // deny all
				Default:     true,
			}).Create()
		}
	}

}
