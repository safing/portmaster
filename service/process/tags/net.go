package tags

import (
	"github.com/safing/portmaster/service/process"
	"github.com/safing/portmaster/service/profile"
)

func init() {
	err := process.RegisterTagHandler(new(NetworkHandler))
	if err != nil {
		panic(err)
	}
}

const (
	netName     = "Network"
	netIPTagKey = "ip"
)

// NetworkHandler handles AppImage processes on Unix systems.
type NetworkHandler struct{}

// Name returns the tag handler name.
func (h *NetworkHandler) Name() string {
	return netName
}

// TagDescriptions returns a list of all possible tags and their description
// of this handler.
func (h *NetworkHandler) TagDescriptions() []process.TagDescription {
	return []process.TagDescription{
		{
			ID:          netIPTagKey,
			Name:        "IP Address",
			Description: "The remote IP address of external requests to Portmaster, if enabled.",
		},
	}
}

// AddTags adds tags to the given process.
func (h *NetworkHandler) AddTags(p *process.Process) {
	// The "net" tag is added directly when creating the virtual process.
}

// CreateProfile creates a profile based on the tags of the process.
// Returns nil to skip.
func (h *NetworkHandler) CreateProfile(p *process.Process) *profile.Profile {
	for _, tag := range p.Tags {
		if tag.Key == netIPTagKey {
			return profile.New(&profile.Profile{
				Source: profile.SourceLocal,
				Name:   p.Name,
				Fingerprints: []profile.Fingerprint{
					{
						Type:      profile.FingerprintTypeTagID,
						Key:       tag.Key,
						Operation: profile.FingerprintOperationEqualsID,
						Value:     tag.Value,
					},
				},
			})
		}
	}
	return nil
}
