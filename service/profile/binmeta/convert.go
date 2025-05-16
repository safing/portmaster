package binmeta

import (
	"bytes"
	"fmt"

	"github.com/fogleman/gg"

	// Import the specialized ICO decoder package
	// This package seems to work better than "github.com/mat/besticon/ico" with ICO files
	// extracted from Windows binaries, particularly those containing cursor-related data
	ico "github.com/sergeymakinen/go-ico"
)

// ConvertICOtoPNG converts a an .ico to a .png image.
func ConvertICOtoPNG(icoBytes []byte) (png []byte, err error) {
	// Decode ICO image.
	// Note: The standard approach with `image.Decode(bytes.NewReader(icoBytes))` sometimes fails
	// when processing certain ICO files (particularly those with cursor data),
	// as it reads initial bytes for format detection before passing the stream to the decoder.
	icon, err := ico.Decode(bytes.NewReader(icoBytes))
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
