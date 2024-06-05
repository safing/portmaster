package binmeta

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/vincent-petithory/dataurl"
	"golang.org/x/exp/slices"

	"github.com/safing/portmaster/base/database"
	"github.com/safing/portmaster/base/database/record"
)

// Icon describes an icon.
type Icon struct {
	Type   IconType
	Value  string
	Source IconSource
}

// IconType describes the type of an Icon.
type IconType string

// Supported icon types.
const (
	IconTypeFile     IconType = "path"
	IconTypeDatabase IconType = "database"
	IconTypeAPI      IconType = "api"
)

func (t IconType) sortOrder() int {
	switch t {
	case IconTypeAPI:
		return 1
	case IconTypeDatabase:
		return 2
	case IconTypeFile:
		return 3
	default:
		return 9
	}
}

// IconSource describes the source of an Icon.
type IconSource string

// Supported icon sources.
const (
	IconSourceUser   IconSource = "user"
	IconSourceImport IconSource = "import"
	IconSourceUI     IconSource = "ui"
	IconSourceCore   IconSource = "core"
)

func (s IconSource) sortOrder() int {
	switch s {
	case IconSourceUser:
		return 10
	case IconSourceImport:
		return 20
	case IconSourceUI:
		return 30
	case IconSourceCore:
		return 40
	default:
		return 90
	}
}

func (icon Icon) sortOrder() int {
	return icon.Source.sortOrder() + icon.Type.sortOrder()
}

// SortAndCompactIcons sorts and compacts a list of icons.
func SortAndCompactIcons(icons []Icon) []Icon {
	// Sort.
	slices.SortFunc[[]Icon, Icon](icons, func(a, b Icon) int {
		aOrder := a.sortOrder()
		bOrder := b.sortOrder()

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

// GetIconAsDataURL returns the icon data as a data URL.
func (icon Icon) GetIconAsDataURL() (bloburl string, err error) {
	switch icon.Type {
	case IconTypeFile:
		return "", errors.New("getting icon from file is not supported")

	case IconTypeDatabase:
		if !strings.HasPrefix(icon.Value, "cache:icons/") {
			return "", errors.New("invalid icon db key")
		}
		r, err := iconDB.Get(icon.Value)
		if err != nil {
			return "", err
		}
		dbIcon, err := EnsureIconInDatabase(r)
		if err != nil {
			return "", err
		}
		return dbIcon.IconData, nil

	case IconTypeAPI:
		data, err := GetProfileIcon(icon.Value)
		if err != nil {
			return "", err
		}
		return dataurl.EncodeBytes(data), nil

	default:
		return "", errors.New("unknown icon type")
	}
}

var iconDB = database.NewInterface(&database.Options{
	Local:    true,
	Internal: true,
})

// IconInDatabase represents an icon saved to the database.
type IconInDatabase struct {
	sync.Mutex
	record.Base

	IconData string `json:"iconData,omitempty"` // DataURL
}

// EnsureIconInDatabase ensures that the given record is a *IconInDatabase, and returns it.
func EnsureIconInDatabase(r record.Record) (*IconInDatabase, error) {
	// unwrap
	if r.IsWrapped() {
		// only allocate a new struct, if we need it
		newIcon := &IconInDatabase{}
		err := record.Unwrap(r, newIcon)
		if err != nil {
			return nil, err
		}
		return newIcon, nil
	}

	// or adjust type
	newIcon, ok := r.(*IconInDatabase)
	if !ok {
		return nil, fmt.Errorf("record not of type *IconInDatabase, but %T", r)
	}
	return newIcon, nil
}
