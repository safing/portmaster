package assets

import (
	_ "embed"
)

// Colored Icon IDs.
const (
	GreenID  = 0
	YellowID = 1
	RedID    = 2
	BlueID   = 3
)

// Icons.
var (
	//go:embed data/icons/pm_light_green_512.ico
	GreenICO []byte

	//go:embed data/icons/pm_light_yellow_512.ico
	YellowICO []byte

	//go:embed data/icons/pm_light_red_512.ico
	RedICO []byte

	//go:embed data/icons/pm_light_blue_512.ico
	BlueICO []byte

	// ColoredIcons holds all the icons as .ICOs
	ColoredIcons = [4][]byte{
		GreenID:  GreenICO,
		YellowID: YellowICO,
		RedID:    RedICO,
		BlueID:   BlueICO,
	}
)

// ScaleColoredIconsTo scales all colored icons to the given size.
// It must be called before any colored icons are used.
// It does nothing on Windows.
func ScaleColoredIconsTo(pixelSize int) {}
