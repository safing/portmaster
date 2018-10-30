package profile

import "sync"

var (
	globalProfile  *Profile
	defaultProfile *Profile

	specialProfileLock sync.RWMutex
)

// FIXME: subscribe to changes and update profiles
