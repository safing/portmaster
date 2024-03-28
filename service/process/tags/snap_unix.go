package tags

import (
	"strings"

	"github.com/safing/portmaster/service/process"
	"github.com/safing/portmaster/service/profile"
	"github.com/safing/portmaster/service/profile/binmeta"
)

func init() {
	err := process.RegisterTagHandler(new(SnapHandler))
	if err != nil {
		panic(err)
	}
}

const (
	snapName       = "Snap"
	snapNameKey    = "snap-name"
	snapVersionKey = "snap-version"

	snapBaseDir = "/snap/"
)

// SnapHandler handles Snap processes on Unix systems.
type SnapHandler struct{}

// Name returns the tag handler name.
func (h *SnapHandler) Name() string {
	return snapName
}

// TagDescriptions returns a list of all possible tags and their description
// of this handler.
func (h *SnapHandler) TagDescriptions() []process.TagDescription {
	return []process.TagDescription{
		{
			ID:          snapNameKey,
			Name:        "Snap Name",
			Description: "Name of snap package.",
		},
		{
			ID:          snapVersionKey,
			Name:        "Snap Version",
			Description: "Version and revision of the snap package.",
		},
	}
}

// AddTags adds tags to the given process.
func (h *SnapHandler) AddTags(p *process.Process) {
	// Check for snap env and verify location.
	snapPkgBaseDir, ok := p.Env["SNAP"]
	if ok && strings.HasPrefix(p.Path, snapPkgBaseDir) {
		// Try adding tags from env.
		added := h.addTagsFromEnv(p)
		if added {
			return
		}
	}

	// Attempt adding tags from path instead, if env did not work out.
	h.addTagsFromPath(p)
}

func (h *SnapHandler) addTagsFromEnv(p *process.Process) (added bool) {
	// Get and verify snap metadata.
	snapPkgName, ok := p.Env["SNAP_NAME"]
	if !ok {
		return false
	}
	snapPkgVersion, ok := p.Env["SNAP_VERSION"]
	if !ok {
		return false
	}

	// Add snap tags.
	p.Tags = append(p.Tags, profile.Tag{
		Key:   snapNameKey,
		Value: snapPkgName,
	})
	p.Tags = append(p.Tags, profile.Tag{
		Key:   snapVersionKey,
		Value: snapPkgVersion,
	})

	return true
}

func (h *SnapHandler) addTagsFromPath(p *process.Process) {
	// Check if the binary is within the snap base dir.
	if !strings.HasPrefix(p.Path, snapBaseDir) {
		return
	}

	// Get snap package name from path.
	splitted := strings.SplitN(strings.TrimPrefix(p.Path, snapBaseDir), "/", 2)
	if len(splitted) < 2 || splitted[0] == "" {
		return
	}

	// Add snap tags.
	p.Tags = append(p.Tags, profile.Tag{
		Key:   snapNameKey,
		Value: splitted[0],
	})
}

// CreateProfile creates a profile based on the tags of the process.
// Returns nil to skip.
func (h *SnapHandler) CreateProfile(p *process.Process) *profile.Profile {
	if tag, ok := p.GetTag(snapNameKey); ok {
		// Check if we have the snap version.
		// Only use presentation path if we have it.
		_, hasVersion := p.GetTag(snapVersionKey)

		return profile.New(&profile.Profile{
			Source:              profile.SourceLocal,
			Name:                binmeta.GenerateBinaryNameFromPath(tag.Value),
			PresentationPath:    p.Path,
			UsePresentationPath: hasVersion,
			Fingerprints: []profile.Fingerprint{
				{
					Type:      profile.FingerprintTypeTagID,
					Key:       tag.Key,
					Operation: profile.FingerprintOperationEqualsID,
					Value:     tag.Value, // Value of snapNameKey.
				},
			},
		})
	}

	return nil
}
