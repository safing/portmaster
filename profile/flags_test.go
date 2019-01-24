package profile

import (
	"testing"

	"github.com/Safing/portmaster/status"
)

func TestProfileFlags(t *testing.T) {

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

	testFlags := Flags{
		Prompt:        status.SecurityLevelsAll,
		Internet:      status.SecurityLevelsDynamicAndSecure,
		LAN:           status.SecurityLevelsDynamicAndSecure,
		Localhost:     status.SecurityLevelsAll,
		Related:       status.SecurityLevelDynamic,
		RequireGate17: status.SecurityLevelsSecureAndFortress,
	}

	if testFlags.String() != "[Prompt, Internet++-, LAN++-, Localhost, Related+--, RequireGate17-++]" {
		t.Errorf("unexpected output: %s", testFlags.String())
	}

	// // check Has
	// emptyFlags := ProfileFlags{}
	// for flag, name := range flagNames {
	// 	if !sortedFlags.Has(flag) {
	// 		t.Errorf("sortedFlags should have flag %s (%d)", name, flag)
	// 	}
	// 	if emptyFlags.Has(flag) {
	// 		t.Errorf("emptyFlags should not have flag %s (%d)", name, flag)
	// 	}
	// }
	//
	// // check ProfileFlags creation from strings
	// var allFlagStrings []string
	// for _, flag := range *sortedFlags {
	// 	allFlagStrings = append(allFlagStrings, flagNames[flag])
	// }
	// newFlags, err := FlagsFromNames(allFlagStrings)
	// if err != nil {
	// 	t.Errorf("error while parsing flags: %s", err)
	// }
	// if newFlags.String() != sortedFlags.String() {
	// 	t.Errorf("parsed flags are not correct (or tests have not been updated to reflect the right number), expected %v, got %v", *sortedFlags, *newFlags)
	// }
	//
	// // check ProfileFlags Stringer
	// flagString := newFlags.String()
	// check := strings.Join(allFlagStrings, ",")
	// if flagString != check {
	// 	t.Errorf("flag string is not correct, expected %s, got %s", check, flagString)
	// }

}
