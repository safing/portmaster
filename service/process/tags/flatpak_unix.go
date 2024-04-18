package tags

import (
	"strings"

	"github.com/safing/portmaster/service/process"
	"github.com/safing/portmaster/service/profile"
	"github.com/safing/portmaster/service/profile/binmeta"
)

func init() {
	err := process.RegisterTagHandler(new(flatpakHandler))
	if err != nil {
		panic(err)
	}
}

const (
	flatpakName     = "Flatpak"
	flatpakIDTagKey = "flatpak-id"
)

// flatpakHandler handles flatpak processes on Unix systems.
type flatpakHandler struct{}

// Name returns the tag handler name.
func (h *flatpakHandler) Name() string {
	return flatpakName
}

// TagDescriptions returns a list of all possible tags and their description
// of this handler.
func (h *flatpakHandler) TagDescriptions() []process.TagDescription {
	return []process.TagDescription{
		{
			ID:          flatpakIDTagKey,
			Name:        "Flatpak ID",
			Description: "ID of the flatpak.",
		},
	}
}

// AddTags adds tags to the given process.
func (h *flatpakHandler) AddTags(p *process.Process) {
	// Check if binary lives in the /app space.
	if !strings.HasPrefix(p.Path, "/app/") {
		return
	}

	// Get the Flatpak ID.
	flatpakID, ok := p.Env["FLATPAK_ID"]
	if !ok || flatpakID == "" {
		return
	}

	// Add matching path for regular profile matching.
	p.MatchingPath = p.Path

	// Add app image tag.
	p.Tags = append(p.Tags, profile.Tag{
		Key:   flatpakIDTagKey,
		Value: flatpakID,
	})
}

// CreateProfile creates a profile based on the tags of the process.
// Returns nil to skip.
func (h *flatpakHandler) CreateProfile(p *process.Process) *profile.Profile {
	if tag, ok := p.GetTag(flatpakIDTagKey); ok {
		return profile.New(&profile.Profile{
			Source:              profile.SourceLocal,
			Name:                binmeta.GenerateBinaryNameFromPath(p.Path),
			PresentationPath:    p.Path,
			UsePresentationPath: true,
			Fingerprints: []profile.Fingerprint{
				{
					Type:      profile.FingerprintTypeTagID,
					Key:       tag.Key,
					Operation: profile.FingerprintOperationEqualsID,
					Value:     tag.Value, // Value of flatpakIDTagKey.
				},
			},
		})
	}

	return nil
}
