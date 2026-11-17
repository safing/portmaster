package config

import (
	"sync/atomic"

	"github.com/tevino/abool"

	"github.com/safing/portmaster/base/log"
)

type (
	// StringOption defines the returned function by GetAsString.
	StringOption func() string
	// StringArrayOption defines the returned function by GetAsStringArray.
	StringArrayOption func() []string
	// IntOption defines the returned function by GetAsInt.
	IntOption func() int64
	// BoolOption defines the returned function by GetAsBool.
	BoolOption func() bool
)

func getValueCache(name string, option *Option, requestedType OptionType) (*Option, *valueCache) {
	// get option
	if option == nil {
		var err error
		option, err = GetOption(name)
		if err != nil {
			log.Errorf("config: request for unregistered option: %s", name)
			return nil, nil
		}
	}

	// Check the option type, no locking required as
	// OptType is immutable once it is set
	if requestedType != option.OptType {
		log.Errorf("config: bad type: requested %s as %s, but is %s", name, getTypeName(requestedType), getTypeName(option.OptType))
		return option, nil
	}

	option.Lock()
	defer option.Unlock()

	// check release level
	if option.ReleaseLevel <= getReleaseLevel() && option.activeValue != nil {
		return option, option.activeValue
	}

	if option.activeDefaultValue != nil {
		return option, option.activeDefaultValue
	}

	return option, option.activeFallbackValue
}

// cachedValue is an immutable snapshot of an option value, valid as long as
// its validity flag is set.
type cachedValue[T any] struct {
	valid  *abool.AtomicBool
	option *Option
	value  T
}

// newCachedGetter returns a lock-free getter for the given option.
// The cached state is swapped atomically, so the returned function is safe
// for concurrent use and never exposes a torn string or slice value.
func newCachedGetter[T any](name string, optType OptionType, fallback T, extract func(*valueCache) T) func() T {
	load := func(option *Option) *cachedValue[T] {
		// Get the validity flag first, so that a change during loading
		// invalidates the result and triggers another load.
		c := &cachedValue[T]{
			valid: getValidityFlag(),
			value: fallback,
		}
		var vc *valueCache
		c.option, vc = getValueCache(name, option, optType)
		if vc != nil {
			c.value = extract(vc)
		}
		return c
	}

	var current atomic.Pointer[cachedValue[T]]
	current.Store(load(nil))

	return func() T {
		c := current.Load()
		if !c.valid.IsSet() {
			c = load(c.option)
			current.Store(c)
		}
		return c.value
	}
}

// GetAsString returns a function that returns the wanted string with high performance.
// The returned function is safe for concurrent use.
func GetAsString(name string, fallback string) StringOption {
	return newCachedGetter(name, OptTypeString, fallback, func(vc *valueCache) string { return vc.stringVal })
}

// GetAsStringArray returns a function that returns the wanted string array with high performance.
// The returned function is safe for concurrent use.
func GetAsStringArray(name string, fallback []string) StringArrayOption {
	return newCachedGetter(name, OptTypeStringArray, fallback, func(vc *valueCache) []string { return vc.stringArrayVal })
}

// GetAsInt returns a function that returns the wanted int with high performance.
// The returned function is safe for concurrent use.
func GetAsInt(name string, fallback int64) IntOption {
	return newCachedGetter(name, OptTypeInt, fallback, func(vc *valueCache) int64 { return vc.intVal })
}

// GetAsBool returns a function that returns the wanted bool with high performance.
// The returned function is safe for concurrent use.
func GetAsBool(name string, fallback bool) BoolOption {
	return newCachedGetter(name, OptTypeBool, fallback, func(vc *valueCache) bool { return vc.boolVal })
}

/*
func getAndFindValue(key string) interface{} {
	optionsLock.RLock()
	option, ok := options[key]
	optionsLock.RUnlock()
	if !ok {
		log.Errorf("config: request for unregistered option: %s", key)
		return nil
	}

	return option.findValue()
}
*/

/*
// findValue finds the preferred value in the user or default config.
func (option *Option) findValue() interface{} {
	// lock option
	option.Lock()
	defer option.Unlock()

	if option.ReleaseLevel <= getReleaseLevel() && option.activeValue != nil {
		return option.activeValue
	}

	if option.activeDefaultValue != nil {
		return option.activeDefaultValue
	}

	return option.DefaultValue
}
*/
