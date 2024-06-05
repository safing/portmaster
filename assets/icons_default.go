//go:build !windows

package assets

import (
	"bytes"
	_ "embed"
	"fmt"
	"image"
	"image/png"

	"golang.org/x/image/draw"

	"github.com/safing/portmaster/base/log"
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
	//go:embed data/icons/pm_light_green_512.png
	GreenPNG []byte

	//go:embed data/icons/pm_light_yellow_512.png
	YellowPNG []byte

	//go:embed data/icons/pm_light_red_512.png
	RedPNG []byte

	//go:embed data/icons/pm_light_blue_512.png
	BluePNG []byte

	// ColoredIcons holds all the icons as .PNGs.
	ColoredIcons [4][]byte
)

func init() {
	setColoredIcons()
}

func setColoredIcons() {
	ColoredIcons = [4][]byte{
		GreenID:  GreenPNG,
		YellowID: YellowPNG,
		RedID:    RedPNG,
		BlueID:   BluePNG,
	}
}

// ScaleColoredIconsTo scales all colored icons to the given size.
// It must be called before any colored icons are used.
// It does nothing on Windows.
func ScaleColoredIconsTo(pixelSize int) {
	// Scale colored icons only.
	GreenPNG = quickScalePNG(GreenPNG, pixelSize)
	YellowPNG = quickScalePNG(YellowPNG, pixelSize)
	RedPNG = quickScalePNG(RedPNG, pixelSize)
	BluePNG = quickScalePNG(BluePNG, pixelSize)

	// Repopulate colored icons.
	setColoredIcons()
}

func quickScalePNG(imgData []byte, pixelSize int) []byte {
	scaledImage, err := scalePNGTo(imgData, pixelSize)
	if err != nil {
		log.Warningf("failed to scale image (using original): %s", err)
		return imgData
	}
	return scaledImage
}

func scalePNGTo(imgData []byte, pixelSize int) ([]byte, error) {
	img, err := png.Decode(bytes.NewReader(imgData))
	if err != nil {
		return nil, fmt.Errorf("failed to decode image: %w", err)
	}

	// Return data unprocessed if image already has the correct size.
	if img.Bounds().Dx() == pixelSize {
		return imgData, nil
	}

	// Scale image to given size.
	rectangle := image.Rect(0, 0, pixelSize, pixelSize)
	scaledImage := image.NewRGBA(rectangle)
	draw.CatmullRom.Scale(scaledImage, rectangle, img, img.Bounds(), draw.Over, nil)

	// Encode scaled image.
	scaledImgBuffer := new(bytes.Buffer)
	err = png.Encode(scaledImgBuffer, scaledImage)
	if err != nil {
		return nil, fmt.Errorf("failed to encode image: %w", err)
	}

	return scaledImgBuffer.Bytes(), nil
}
