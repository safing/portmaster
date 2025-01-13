package updater

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/utils"
)

// ResourceRegistry is a registry for managing update resources.
type ResourceRegistry struct {
	sync.RWMutex

	Name       string
	storageDir *utils.DirStructure
	tmpDir     *utils.DirStructure
	indexes    []*Index
	state      *RegistryState

	resources        map[string]*Resource
	UpdateURLs       []string
	UserAgent        string
	MandatoryUpdates []string
	AutoUnpack       []string

	// Verification holds a map of VerificationOptions assigned to their
	// applicable identifier path prefix.
	// Use an empty string to denote the default.
	// Use empty options to disable verification for a path prefix.
	Verification map[string]*VerificationOptions

	// UsePreReleases signifies that pre-releases should be used when selecting a
	// version. Even if false, a pre-release version will still be used if it is
	// defined as the current version by an index.
	UsePreReleases bool

	// DevMode specifies if a local 0.0.0 version should be always chosen, when available.
	DevMode bool

	// Online specifies if resources may be downloaded if not available locally.
	Online bool

	// StateNotifyFunc may be set to receive any changes to the registry state.
	// The specified function may lock the state, but may not block or take a
	// lot of time.
	StateNotifyFunc func(*RegistryState)
}

// AddIndex adds a new index to the resource registry.
// The order is important, as indexes added later will override the current
// release from earlier indexes.
func (reg *ResourceRegistry) AddIndex(idx Index) {
	reg.Lock()
	defer reg.Unlock()

	// Get channel name from path.
	idx.Channel = strings.TrimSuffix(
		filepath.Base(idx.Path), filepath.Ext(idx.Path),
	)

	reg.indexes = append(reg.indexes, &idx)
}

// PreInitUpdateState sets the initial update state of the registry before initialization.
func (reg *ResourceRegistry) PreInitUpdateState(s UpdateState) error {
	if reg.state != nil {
		return errors.New("registry already initialized")
	}

	reg.state = &RegistryState{
		Updates: s,
	}
	return nil
}

// Initialize initializes a raw registry struct and makes it ready for usage.
func (reg *ResourceRegistry) Initialize(storageDir *utils.DirStructure) error {
	// check if storage dir is available
	err := storageDir.Ensure()
	if err != nil {
		return err
	}

	// set default name
	if reg.Name == "" {
		reg.Name = "updater"
	}

	// initialize private attributes
	reg.storageDir = storageDir
	reg.tmpDir = storageDir.ChildDir("tmp", utils.AdminOnlyPermission)
	reg.resources = make(map[string]*Resource)
	if reg.state == nil {
		reg.state = &RegistryState{}
	}
	reg.state.ID = StateReady
	reg.state.reg = reg

	// remove tmp dir to delete old entries
	err = reg.Cleanup()
	if err != nil {
		log.Warningf("%s: failed to remove tmp dir: %s", reg.Name, err)
	}

	// (re-)create tmp dir
	err = reg.tmpDir.Ensure()
	if err != nil {
		log.Warningf("%s: failed to create tmp dir: %s", reg.Name, err)
	}

	// Check verification options.
	if reg.Verification != nil {
		for prefix, opts := range reg.Verification {
			// Check if verification is disable for this prefix.
			if opts == nil {
				continue
			}

			// If enabled, a trust store is required.
			if opts.TrustStore == nil {
				return fmt.Errorf("verification enabled for prefix %q, but no trust store configured", prefix)
			}

			// DownloadPolicy must be equal or stricter than DiskLoadPolicy.
			if opts.DiskLoadPolicy < opts.DownloadPolicy {
				return errors.New("verification download policy must be equal or stricter than the disk load policy")
			}

			// Warn if all policies are disabled.
			if opts.DownloadPolicy == SignaturePolicyDisable &&
				opts.DiskLoadPolicy == SignaturePolicyDisable {
				log.Warningf("%s: verification enabled for prefix %q, but all policies set to disable", reg.Name, prefix)
			}
		}
	}

	return nil
}

// StorageDir returns the main storage dir of the resource registry.
func (reg *ResourceRegistry) StorageDir() *utils.DirStructure {
	return reg.storageDir
}

// TmpDir returns the temporary working dir of the resource registry.
func (reg *ResourceRegistry) TmpDir() *utils.DirStructure {
	return reg.tmpDir
}

// SetDevMode sets the development mode flag.
func (reg *ResourceRegistry) SetDevMode(on bool) {
	reg.Lock()
	defer reg.Unlock()

	reg.DevMode = on
}

// SetUsePreReleases sets the UsePreReleases flag.
func (reg *ResourceRegistry) SetUsePreReleases(yes bool) {
	reg.Lock()
	defer reg.Unlock()

	reg.UsePreReleases = yes
}

// AddResource adds a resource to the registry. Does _not_ select new version.
func (reg *ResourceRegistry) AddResource(identifier, version string, index *Index, available, currentRelease, preRelease bool) error {
	reg.Lock()
	defer reg.Unlock()

	err := reg.addResource(identifier, version, index, available, currentRelease, preRelease)
	return err
}

func (reg *ResourceRegistry) addResource(identifier, version string, index *Index, available, currentRelease, preRelease bool) error {
	res, ok := reg.resources[identifier]
	if !ok {
		res = reg.newResource(identifier)
		reg.resources[identifier] = res
	}
	res.Index = index

	return res.AddVersion(version, available, currentRelease, preRelease)
}

// AddResources adds resources to the registry. Errors are logged, the last one is returned. Despite errors, non-failing resources are still added. Does _not_ select new versions.
func (reg *ResourceRegistry) AddResources(versions map[string]string, index *Index, available, currentRelease, preRelease bool) error {
	reg.Lock()
	defer reg.Unlock()

	// add versions and their flags to registry
	var lastError error
	for identifier, version := range versions {
		lastError = reg.addResource(identifier, version, index, available, currentRelease, preRelease)
		if lastError != nil {
			log.Warningf("%s: failed to add resource %s: %s", reg.Name, identifier, lastError)
		}
	}

	return lastError
}

// SelectVersions selects new resource versions depending on the current registry state.
func (reg *ResourceRegistry) SelectVersions() {
	reg.RLock()
	defer reg.RUnlock()

	for _, res := range reg.resources {
		res.Lock()
		res.selectVersion()
		res.Unlock()
	}
}

// GetSelectedVersions returns a list of the currently selected versions.
func (reg *ResourceRegistry) GetSelectedVersions() (versions map[string]string) {
	reg.RLock()
	defer reg.RUnlock()

	for _, res := range reg.resources {
		res.Lock()
		versions[res.Identifier] = res.SelectedVersion.VersionNumber
		res.Unlock()
	}

	return
}

// Purge deletes old updates, retaining a certain amount, specified by the keep
// parameter. Will at least keep 2 updates per resource.
func (reg *ResourceRegistry) Purge(keep int) {
	reg.RLock()
	defer reg.RUnlock()

	for _, res := range reg.resources {
		res.Purge(keep)
	}
}

// ResetResources removes all resources from the registry.
func (reg *ResourceRegistry) ResetResources() {
	reg.Lock()
	defer reg.Unlock()

	reg.resources = make(map[string]*Resource)
}

// ResetIndexes removes all indexes from the registry.
func (reg *ResourceRegistry) ResetIndexes() {
	reg.Lock()
	defer reg.Unlock()

	reg.indexes = make([]*Index, 0, len(reg.indexes))
}

// Cleanup removes temporary files.
func (reg *ResourceRegistry) Cleanup() error {
	// delete download tmp dir
	return os.RemoveAll(reg.tmpDir.Path)
}
