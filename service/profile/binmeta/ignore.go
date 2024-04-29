package binmeta

import (
	"strings"
)

var ignoreIcons = map[string]struct{}{
	// Windows Default Icons.
	"a27898ddfa4e0481b62c69faa196919a738fcade": {},
	"5a3eea8bcd08b9336ce9c5083f26185164268ee9": {},
	"573393d6ad238d255b20dc1c1b303c95debe6965": {},
	"d459b2cb23c27cc31ccab5025533048d5d8301bf": {},
	"d35a0d91ebfda81df5286f68ec5ddb1d6ad6b850": {},
	"cc33187385498384f1b648e23be5ef1a2e9f5f71": {},
}

// IgnoreIcon returns whether an icon should be ignored or not.
func IgnoreIcon(name string) bool {
	// Make lower case.
	name = strings.ToLower(name)
	// Remove extension.
	extIndex := strings.Index(name, ".")
	if extIndex > 0 {
		name = name[:extIndex]
	}

	// Check if ID is in list.
	_, found := ignoreIcons[name]
	return found
}
