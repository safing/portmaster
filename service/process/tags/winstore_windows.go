package tags

import (
	"os"
	"strings"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/utils"
	"github.com/safing/portmaster/service/process"
	"github.com/safing/portmaster/service/profile"
	"github.com/safing/portmaster/service/profile/binmeta"
)

func init() {
	err := process.RegisterTagHandler(new(WinStoreHandler))
	if err != nil {
		panic(err)
	}

	// Add custom WindowsApps path.
	customWinStorePath := os.ExpandEnv(`%ProgramFiles%\WindowsApps\`)
	if !utils.StringInSlice(winStorePaths, customWinStorePath) {
		winStorePaths = append(winStorePaths, customWinStorePath)
	}
}

const (
	winStoreName              = "Windows Store"
	winStoreAppNameTagKey     = "winstore-app-name"
	winStorePublisherIDTagKey = "winstore-publisher-id"
)

var winStorePaths = []string{`C:\Program Files\WindowsApps\`}

// WinStoreHandler handles Windows Store Apps.
type WinStoreHandler struct{}

// Name returns the tag handler name.
func (h *WinStoreHandler) Name() string {
	return winStoreName
}

// TagDescriptions returns a list of all possible tags and their description
// of this handler.
func (h *WinStoreHandler) TagDescriptions() []process.TagDescription {
	return []process.TagDescription{
		{
			ID:          winStoreAppNameTagKey,
			Name:        "Windows Store App Name",
			Description: "Name of the Windows Store App, as found in the executable path.",
		},
		{
			ID:          winStorePublisherIDTagKey,
			Name:        "Windows Store Publisher ID",
			Description: "Publisher ID of a Windows Store App.",
		},
	}
}

// AddTags adds tags to the given process.
func (h *WinStoreHandler) AddTags(p *process.Process) {
	// Check if the path is in one of the Windows Store Apps paths.
	var appDir string
	for _, winStorePath := range winStorePaths {
		if strings.HasPrefix(p.Path, winStorePath) {
			appDir = strings.SplitN(strings.TrimPrefix(p.Path, winStorePath), `\`, 2)[0]
			break
		}
	}
	if appDir == "" {
		return
	}

	// Extract information from path.
	// Example: Microsoft.Office.OneNote_17.6769.57631.0_x64__8wekyb3d8bbwe
	splitted := strings.Split(appDir, "_")
	if len(splitted) != 5 { // Four fields, one "__".
		log.Debugf("profile/tags: windows store app has incompatible app dir format: %q", appDir)
		return
	}

	name := splitted[0]
	// version  := splitted[1]
	// platform  := splitted[2]
	publisherID := splitted[4]

	// Add tags.
	p.Tags = append(p.Tags, profile.Tag{
		Key:   winStoreAppNameTagKey,
		Value: name,
	})
	p.Tags = append(p.Tags, profile.Tag{
		Key:   winStorePublisherIDTagKey,
		Value: publisherID,
	})
}

// CreateProfile creates a profile based on the tags of the process.
// Returns nil to skip.
func (h *WinStoreHandler) CreateProfile(p *process.Process) *profile.Profile {
	if tag, ok := p.GetTag(winStoreAppNameTagKey); ok {
		return profile.New(&profile.Profile{
			Source:              profile.SourceLocal,
			Name:                binmeta.GenerateBinaryNameFromPath(tag.Value),
			PresentationPath:    p.Path,
			UsePresentationPath: true,
			Fingerprints: []profile.Fingerprint{
				{
					Type:      profile.FingerprintTypeTagID,
					Key:       tag.Key,
					Operation: profile.FingerprintOperationEqualsID,
					Value:     tag.Value, // Value of winStoreAppNameTagKey.
				},
			},
		})
	}

	return nil
}
