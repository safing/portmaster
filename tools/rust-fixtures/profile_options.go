package main

import (
	"strings"
	_ "unsafe" // Required for go:linkname.

	"github.com/safing/portmaster/base/config"
	_ "github.com/safing/portmaster/service/profile"
)

// registerProfileConfiguration is the unexported registration of the profile
// package; linking to it avoids copying the option definitions.
//
//go:linkname registerProfileConfiguration github.com/safing/portmaster/service/profile.registerConfiguration
func registerProfileConfiguration() error

// genProfileOptions records the options registered by the profile package,
// sorted by key like config.ExportOptions.
func genProfileOptions() {
	if err := registerProfileConfiguration(); err != nil {
		fail(err)
	}

	var definitions []*config.Option
	for _, option := range config.ExportOptions() {
		for _, prefix := range []string{"filter/", "history/", "spn/", "splittun/"} {
			if strings.HasPrefix(option.Key, prefix) {
				definitions = append(definitions, option)
				break
			}
		}
	}
	writeObject("profile_options.json", definitions)
}
