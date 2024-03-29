package process

import (
	"errors"
	"sync"

	"github.com/safing/portmaster/service/profile"
)

var (
	tagRegistry     []TagHandler
	tagRegistryLock sync.RWMutex
)

// TagHandler is a collection of process tag related interfaces.
type TagHandler interface {
	// Name returns the tag handler name.
	Name() string

	// TagDescriptions returns a list of all possible tags and their description
	// of this handler.
	TagDescriptions() []TagDescription

	// AddTags adds tags to the given process.
	AddTags(p *Process)

	// CreateProfile creates a profile based on the tags of the process.
	// Returns nil to skip.
	CreateProfile(p *Process) *profile.Profile
}

// TagDescription describes a tag.
type TagDescription struct {
	ID          string
	Name        string
	Description string
}

// RegisterTagHandler registers a tag handler.
func RegisterTagHandler(th TagHandler) error {
	tagRegistryLock.Lock()
	defer tagRegistryLock.Unlock()

	// Check if the handler is already registered.
	for _, existingTH := range tagRegistry {
		if th.Name() == existingTH.Name() {
			return errors.New("already registered")
		}
	}

	tagRegistry = append(tagRegistry, th)
	return nil
}

func (p *Process) addTags() {
	tagRegistryLock.RLock()
	defer tagRegistryLock.RUnlock()

	for _, th := range tagRegistry {
		th.AddTags(p)
	}
}

// CreateProfileCallback attempts to create a profile on special attributes
// of the process.
func (p *Process) CreateProfileCallback() *profile.Profile {
	tagRegistryLock.RLock()
	defer tagRegistryLock.RUnlock()

	// Go through handlers and see which one wants to create a profile.
	for _, th := range tagRegistry {
		newProfile := th.CreateProfile(p)
		if newProfile != nil {
			return newProfile
		}
	}

	// No handler wanted to create a profile.
	return nil
}
