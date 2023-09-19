package profile

import (
	"strings"

	"golang.org/x/exp/slices"
)

// Icon describes an icon.
type Icon struct {
	Type  IconType
	Value string
}

// IconType describes the type of an Icon.
type IconType string

// Supported icon types.
const (
	IconTypeFile     IconType = "path"
	IconTypeDatabase IconType = "database"
)

func (t IconType) sortOrder() int {
	switch t {
	case IconTypeDatabase:
		return 1
	case IconTypeFile:
		return 2
	default:
		return 100
	}
}

func sortAndCompactIcons(icons []Icon) []Icon {
	// Sort.
	slices.SortFunc[[]Icon, Icon](icons, func(a, b Icon) int {
		aOrder := a.Type.sortOrder()
		bOrder := b.Type.sortOrder()

		switch {
		case aOrder != bOrder:
			return aOrder - bOrder
		case a.Value != b.Value:
			return strings.Compare(a.Value, b.Value)
		default:
			return 0
		}
	})

	// De-duplicate.
	icons = slices.CompactFunc[[]Icon, Icon](icons, func(a, b Icon) bool {
		return a.Type == b.Type && a.Value == b.Value
	})

	return icons
}
