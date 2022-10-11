package tags

import (
	"strings"

	"github.com/safing/portbase/utils/osdetail"
	"github.com/safing/portmaster/process"
	"github.com/safing/portmaster/profile"
)

func init() {
	err := process.RegisterTagHandler(new(AppImageHandler))
	if err != nil {
		panic(err)
	}
}

const (
	appImageName       = "AppImage"
	appImagePathTagKey = "app-image-path"
)

// AppImageHandler handles AppImage processes on Unix systems.
type AppImageHandler struct{}

// Name returns the tag handler name.
func (h *AppImageHandler) Name() string {
	return appImageName
}

// TagDescriptions returns a list of all possible tags and their description
// of this handler.
func (h *AppImageHandler) TagDescriptions() []process.TagDescription {
	return []process.TagDescription{
		{
			ID:          appImagePathTagKey,
			Name:        "App Image Path",
			Description: "Path to the app image file itself.",
		},
	}
}

// AddTags adds tags to the given process.
func (h *AppImageHandler) AddTags(p *process.Process) {
	// Get and verify AppImage location.
	appImageLocation, ok := p.Env["APPIMAGE"]
	if !ok {
		return
	}
	appImageMountDir, ok := p.Env["APPDIR"]
	if !ok {
		return
	}
	// Check if the process path is in the mount dir.
	if !strings.HasPrefix(p.Path, appImageMountDir) {
		return
	}

	// Add matching path for regular profile matching.
	p.MatchingPath = appImageLocation

	// Add app image tags.
	p.Tags = append(p.Tags, profile.Tag{
		Key:   appImagePathTagKey,
		Value: appImageLocation,
	})
}

// CreateProfile creates a profile based on the tags of the process.
// Returns nil to skip.
func (h *AppImageHandler) CreateProfile(p *process.Process) *profile.Profile {
	if tag, ok := p.GetTag(appImagePathTagKey); ok {
		return profile.New(&profile.Profile{
			Source:              profile.SourceLocal,
			Name:                osdetail.GenerateBinaryNameFromPath(tag.Value),
			PresentationPath:    p.Path,
			UsePresentationPath: true,
			Fingerprints: []profile.Fingerprint{
				{
					Type:      profile.FingerprintTypePathID,
					Operation: profile.FingerprintOperationEqualsID,
					Value:     tag.Value, // Value of appImagePathTagKey.
				},
			},
		})
	}

	return nil
}
