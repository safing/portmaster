// Copyright Safing ICS Technologies GmbH. Use of this source code is governed by the AGPL license that can be found in the LICENSE file.

package profiles

import (
	"errors"
	"strings"
)

// ProfileFlags are used to quickly add common attributes to profiles
type ProfileFlags []int8

const (
	// Who?
	// System apps must be run by system user, else deny
	System int8 = iota + 1
	// Admin apps must be run by user with admin privileges, else deny
	Admin
	// User apps must be run by user (identified by having an active safing UI), else deny
	User

	// Where?
	// Internet apps may connect to the Internet, if unset, all connections to the Internet are denied
	Internet
	// LocalNet apps may connect to the local network (i.e. private IP address spaces), if unset, all connections to the local network are denied
	LocalNet

	// How?
	// Strict apps may only connect to domains that are related to themselves
	Strict
	// Gateway apps will connect to user-defined servers
	Gateway
	// Browser apps connect to multitudes of different servers and require special handling
	Browser
	// Directconnect apps may connect to any IP without dns association (e.g. P2P apps, network analysis tools)
	Directconnect
	// Service apps may accept incoming connections
	Service
)

var (
	// ErrProfileFlagsParseFailed is returned if a an invalid flag is encountered while parsing
	ErrProfileFlagsParseFailed = errors.New("profiles: failed to parse flags")

	sortedFlags = &ProfileFlags{
		System,
		Admin,
		User,
		Internet,
		LocalNet,
		Strict,
		Gateway,
		Service,
		Directconnect,
		Browser,
	}

	flagIDs = map[string]int8{
		"System":        System,
		"Admin":         Admin,
		"User":          User,
		"Internet":      Internet,
		"LocalNet":      LocalNet,
		"Strict":        Strict,
		"Gateway":       Gateway,
		"Service":       Service,
		"Directconnect": Directconnect,
		"Browser":       Browser,
	}

	flagNames = map[int8]string{
		System:        "System",
		Admin:         "Admin",
		User:          "User",
		Internet:      "Internet",
		LocalNet:      "LocalNet",
		Strict:        "Strict",
		Gateway:       "Gateway",
		Service:       "Service",
		Directconnect: "Directconnect",
		Browser:       "Browser",
	}
)

// FlagsFromNames creates ProfileFlags from a comma seperated list of flagnames (e.g. "System,Strict,Secure")
func FlagsFromNames(words []string) (*ProfileFlags, error) {
	var flags ProfileFlags
	for _, entry := range words {
		flag, ok := flagIDs[entry]
		if !ok {
			return nil, ErrProfileFlagsParseFailed
		}
		flags = append(flags, flag)
	}
	return &flags, nil
}

// Has checks if a ProfileFlags object has a flag
func (pf *ProfileFlags) Has(searchFlag int8) bool {
	for _, flag := range *pf {
		if flag == searchFlag {
			return true
		}
	}
	return false
}

// String return a string representation of ProfileFlags
func (pf *ProfileFlags) String() string {
	var namedFlags []string
	for _, flag := range *pf {
		namedFlags = append(namedFlags, flagNames[flag])
	}
	return strings.Join(namedFlags, ",")
}
