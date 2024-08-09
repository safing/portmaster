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
	//go:embed data/icons/pm_light_green_256.png
	GreenICO []byte

	//go:embed data/icons/pm_light_yellow_256.png
	YellowICO []byte

	//go:embed data/icons/pm_light_red_256.png
	RedICO []byte

	//go:embed data/icons/pm_light_blue_256.png
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
