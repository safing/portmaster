package tags

import (
	"fmt"
	"strings"

	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/process"
	"github.com/safing/portmaster/profile"
)

func init() {
	err := process.RegisterTagHandler(new(SVCHostTagHandler))
	if err != nil {
		panic(err)
	}
}

const (
	svchostName   = "SvcHost"
	svchostTagKey = "svchost"
)

// SVCHostTagHandler handles svchost processes on Windows.
type SVCHostTagHandler struct{}

// Name returns the tag handler name.
func (h *SVCHostTagHandler) Name() string {
	return svcHostName
}

// TagDescriptions returns a list of all possible tags and their description
// of this handler.
func (h *SVCHostTagHandler) TagDescriptions() []process.TagDescription {
	return []process.TagDescription{
		process.TagDescription{
			ID:          svcHostTagKey,
			Name:        "SvcHost Service Name",
			Description: "Name of a service running in svchost.exe as reported by Windows.",
		},
	}
}

// TagKeys returns a list of all possible tag keys of this handler.
func (h *SVCHostTagHandler) TagKeys() []string {
	return []string{svcHostTagKey}
}

// AddTags adds tags to the given process.
func (h *SVCHostTagHandler) AddTags(p *process.Process) {
	// Check for svchost.exe.
	if p.ExecName != "svchost.exe" {
		return
	}

	// Get services of svchost instance.
	svcNames, err := osdetail.GetServiceNames(int32(p.Pid))
	switch err {
	case nil:
		// Append service names to process name.
		p.Name += fmt.Sprintf(" (%s)", strings.Join(svcNames, ", "))
		// Add services as tags.
		for _, svcName := range svcNames {
			p.Tag = append(p.Tag, profile.Tag{
				Key:   svchostTagKey,
				Value: svcName,
			})
		}
	case osdetail.ErrServiceNotFound:
		log.Tracef("process/tags: failed to get service name for svchost.exe (pid %d): %s", p.Pid, err)
	default:
		log.Warningf("process/tags: failed to get service name for svchost.exe (pid %d): %s", p.Pid, err)
	}
}

// CreateProfile creates a profile based on the tags of the process.
// Returns nil to skip.
func (h *SVCHostTagHandler) CreateProfile(p *process.Process) *profile.Profile {
	for _, tag := range p.Tags {
		if tag.Key == svchostTagKey {
			return profile.New(
				profile.SourceLocal,
				"",
				"Windows Service: "+tag.Value,
				p.Path,
				[]profile.Fingerprint{profile.Fingerprint{
					Type:      profile.FingerprintTypeTagID,
					Key:       tag.Key,
					Operation: profile.FingerprintOperationEqualsID,
					Value:     tag.Value,
				}},
				nil,
			)
		}
	}
	return nil
}
