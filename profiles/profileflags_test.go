// Copyright Safing ICS Technologies GmbH. Use of this source code is governed by the AGPL license that can be found in the LICENSE file.

package profiles

import (
	"strings"
	"testing"
)

func TestProfileFlags(t *testing.T) {

	// check if SYSTEM is 1
	if System != 1 {
		t.Errorf("System ist first const and must be 1")
	}
	if Admin != 2 {
		t.Errorf("Admin ist second const and must be 2")
	}

	// check if all IDs have a name
	for key, entry := range flagIDs {
		if _, ok := flagNames[entry]; !ok {
			t.Errorf("could not find entry for %s in flagNames", key)
		}
	}

	// check if all names have an ID
	for key, entry := range flagNames {
		if _, ok := flagIDs[entry]; !ok {
			t.Errorf("could not find entry for %d in flagNames", key)
		}
	}

	// check Has
	emptyFlags := ProfileFlags{}
	for flag, name := range flagNames {
		if !sortedFlags.Has(flag) {
			t.Errorf("sortedFlags should have flag %s (%d)", name, flag)
		}
		if emptyFlags.Has(flag) {
			t.Errorf("emptyFlags should not have flag %s (%d)", name, flag)
		}
	}

	// check ProfileFlags creation from strings
	var allFlagStrings []string
	for _, flag := range *sortedFlags {
		allFlagStrings = append(allFlagStrings, flagNames[flag])
	}
	newFlags, err := FlagsFromNames(allFlagStrings)
	if err != nil {
		t.Errorf("error while parsing flags: %s", err)
	}
	if newFlags.String() != sortedFlags.String() {
		t.Errorf("parsed flags are not correct (or tests have not been updated to reflect the right number), expected %v, got %v", *sortedFlags, *newFlags)
	}

	// check ProfileFlags Stringer
	flagString := newFlags.String()
	check := strings.Join(allFlagStrings, ",")
	if flagString != check {
		t.Errorf("flag string is not correct, expected %s, got %s", check, flagString)
	}

}
