package binmeta

import (
	"bytes"
	"fmt"
	"image"
	_ "image/png" // Register png support for image package

	"github.com/fogleman/gg"
	_ "github.com/mat/besticon/ico" // Register ico support for image package
)

// ConvertICOtoPNG converts a an .ico to a .png image.
func ConvertICOtoPNG(ico []byte) (png []byte, err error) {
	// Decode the ICO.
	icon, _, err := image.Decode(bytes.NewReader(ico))
	if err != nil {
		return nil, fmt.Errorf("failed to decode ICO: %w", err)
	}

	// Convert to raw image.
	img := gg.NewContextForImage(icon)

	// Convert to PNG.
	imgBuf := &bytes.Buffer{}
	err = img.EncodePNG(imgBuf)
	if err != nil {
		return nil, fmt.Errorf("failed to encode PNG: %w", err)
	}

	return imgBuf.Bytes(), nil
}
