package tags

import (
	"bufio"
	"bytes"
	"os"
	"regexp"
	"strings"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/process"
	"github.com/safing/portmaster/service/profile"
	"github.com/safing/portmaster/service/profile/binmeta"
)

func init() {
	err := process.RegisterTagHandler(new(AppImageHandler))
	if err != nil {
		panic(err)
	}
}

const (
	appImageName          = "AppImage"
	appImagePathTagKey    = "app-image-path"
	appImageMountIDTagKey = "app-image-mount-id"
)

var (
	appImageMountDirRegex         = regexp.MustCompile(`^/tmp/.mount_[^/]+`)
	appImageMountNameExtractRegex = regexp.MustCompile(`^[A-Za-z0-9]+`)
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
			Name:        "AppImage Path",
			Description: "Path to the app image file itself.",
		},
		{
			ID:          appImageMountIDTagKey,
			Name:        "AppImage Mount ID",
			Description: "Extracted ID from the AppImage mount name. Use AppImage Path instead, if available.",
		},
	}
}

// AddTags adds tags to the given process.
func (h *AppImageHandler) AddTags(p *process.Process) {
	// Detect app image path via ENV vars.
	func() {
		// Get and verify AppImage location.
		appImageLocation, ok := p.Env["APPIMAGE"]
		if !ok || appImageLocation == "" {
			return
		}
		appImageMountDir, ok := p.Env["APPDIR"]
		if !ok || appImageMountDir == "" {
			return
		}
		// Check if the process path is in the mount dir.
		if !strings.HasPrefix(p.Path, appImageMountDir) {
			return
		}

		// Add matching path for regular profile matching.
		p.MatchingPath = appImageLocation

		// Add app image tag.
		p.Tags = append(p.Tags, profile.Tag{
			Key:   appImagePathTagKey,
			Value: appImageLocation,
		})
	}()

	// Detect app image mount point.
	func() {
		// Check if binary path matches app image mount pattern.
		mountDir := appImageMountDirRegex.FindString(p.Path)
		if mountDir == "" {
			return
		}

		// Get mount name of mount dir.
		// Also, this confirm this is actually a mounted dir.
		mountName, err := getAppImageMountName(mountDir)
		if err != nil {
			log.Debugf("process/tags: failed to get mount name: %s", err)
			return
		}
		if mountName == "" {
			return
		}

		// Extract a usable ID from the mount name.
		mountName, _ = strings.CutPrefix(mountName, "gearlever_")
		mountName = appImageMountNameExtractRegex.FindString(mountName)
		if mountName == "" {
			return
		}

		// Add app image tag.
		p.Tags = append(p.Tags, profile.Tag{
			Key:   appImageMountIDTagKey,
			Value: mountName,
		})
	}()
}

// CreateProfile creates a profile based on the tags of the process.
// Returns nil to skip.
func (h *AppImageHandler) CreateProfile(p *process.Process) *profile.Profile {
	if tag, ok := p.GetTag(appImagePathTagKey); ok {
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
					Value:     tag.Value, // Value of appImagePathTagKey.
				},
			},
		})
	}

	if tag, ok := p.GetTag(appImageMountIDTagKey); ok {
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
					Value:     tag.Value, // Value of appImageMountIDTagKey.
				},
			},
		})
	}

	return nil
}

func getAppImageMountName(mountPoint string) (mountName string, err error) {
	// Get mounts.
	data, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return "", err
	}

	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 2 {
			switch {
			case fields[1] != mountPoint:
			case !strings.HasSuffix(strings.ToLower(fields[0]), ".appimage"):
			default:
				// Found AppImage mount!
				return fields[0], nil
			}
		}
	}
	if scanner.Err() != nil {
		return "", scanner.Err()
	}

	return "", nil
}
