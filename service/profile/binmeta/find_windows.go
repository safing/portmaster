package binmeta

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/tc-hib/winres"
	"github.com/tc-hib/winres/version"
)

// GetIconAndName returns an icon and name of the given binary path.
// Providing the home directory of the user running the process of that binary can improve results.
// Even if an error is returned, the other return values are valid, if set.
func GetIconAndName(ctx context.Context, binPath string, homeDir string) (icon *Icon, name string, err error) {
	// Get name and png from exe.
	png, name, err := getIconAndNamefromRSS(ctx, binPath)

	// Fall back to name generation if name is not set.
	if name == "" {
		name = GenerateBinaryNameFromPath(binPath)
	}

	// Handle previous error.
	if err != nil {
		return nil, name, err
	}

	// Update profile icon and return icon object.
	filename, err := UpdateProfileIcon(png, "png")
	if err != nil {
		return nil, name, fmt.Errorf("failed to store icon: %w", err)
	}

	return &Icon{
		Type:   IconTypeAPI,
		Value:  filename,
		Source: IconSourceCore,
	}, name, nil
}

func getIconAndNamefromRSS(ctx context.Context, binPath string) (png []byte, name string, err error) {
	// Open .exe file.
	exeFile, err := os.Open(binPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, "", nil
		}
		return nil, "", fmt.Errorf("failed to open exe %s to get icon: %w", binPath, err)
	}
	defer exeFile.Close() //nolint:errcheck

	// Load .exe resources.
	rss, err := winres.LoadFromEXE(exeFile)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get rss: %w", err)
	}

	// DEBUG: Print all available resources:
	// rss.Walk(func(typeID, resID winres.Identifier, langID uint16, data []byte) bool {
	// 	fmt.Printf("typeID=%d resID=%d langID=%d\n", typeID, resID, langID)
	// 	return true
	// })

	// Get name from version record.
	var (
		versionInfo    *version.Info
		versionInfoErr error
	)
	rss.WalkType(winres.RT_VERSION, func(resID winres.Identifier, langID uint16, data []byte) bool {
		versionInfo, versionInfoErr = version.FromBytes(data)
		switch {
		case versionInfoErr != nil:
			return true
		case versionInfo == nil:
			return true
		}

		// Get metadata table and main language.
		table := versionInfo.Table().GetMainTranslation()
		if table == nil {
			return true
		}

		name = table[version.ProductName]
		return name == ""
	})
	name = cleanFileDescription(name)

	// Get first icon.
	var (
		icon    *winres.Icon
		iconErr error
	)
	rss.WalkType(winres.RT_GROUP_ICON, func(resID winres.Identifier, langID uint16, _ []byte) bool {
		icon, iconErr = rss.GetIconTranslation(resID, langID)
		return iconErr != nil
	})
	if iconErr != nil {
		return nil, name, fmt.Errorf("failed to get icon: %w", err)
	}
	if icon == nil {
		return nil, name, errors.New("no icon in resources")
	}
	// Convert icon, if it exists.
	icoBuf := &bytes.Buffer{}
	err = icon.SaveICO(icoBuf)
	if err != nil {
		return nil, name, fmt.Errorf("failed to save ico: %w", err)
	}
	png, err = ConvertICOtoPNG(icoBuf.Bytes())
	if err != nil {
		return nil, name, fmt.Errorf("failed to convert ico to png: %w", err)
	}

	return png, name, nil
}
