package config

import "sync"

type safe struct{}

// Concurrent makes concurrency safe get methods available.
var Concurrent = &safe{}

// GetAsString returns a function that returns the wanted string with high performance.
func (cs *safe) GetAsString(name string, fallback string) StringOption {
	valid := getValidityFlag()
	option, valueCache := getValueCache(name, nil, OptTypeString)
	value := fallback
	if valueCache != nil {
		value = valueCache.stringVal
	}
	var lock sync.Mutex

	return func() string {
		lock.Lock()
		defer lock.Unlock()
		if !valid.IsSet() {
			valid = getValidityFlag()
			option, valueCache = getValueCache(name, option, OptTypeString)
			if valueCache != nil {
				value = valueCache.stringVal
			} else {
				value = fallback
			}
		}
		return value
	}
}

// GetAsStringArray returns a function that returns the wanted string with high performance.
func (cs *safe) GetAsStringArray(name string, fallback []string) StringArrayOption {
	valid := getValidityFlag()
	option, valueCache := getValueCache(name, nil, OptTypeStringArray)
	value := fallback
	if valueCache != nil {
		value = valueCache.stringArrayVal
	}
	var lock sync.Mutex

	return func() []string {
		lock.Lock()
		defer lock.Unlock()
		if !valid.IsSet() {
			valid = getValidityFlag()
			option, valueCache = getValueCache(name, option, OptTypeStringArray)
			if valueCache != nil {
				value = valueCache.stringArrayVal
			} else {
				value = fallback
			}
		}
		return value
	}
}

// GetAsInt returns a function that returns the wanted int with high performance.
func (cs *safe) GetAsInt(name string, fallback int64) IntOption {
	valid := getValidityFlag()
	option, valueCache := getValueCache(name, nil, OptTypeInt)
	value := fallback
	if valueCache != nil {
		value = valueCache.intVal
	}
	var lock sync.Mutex

	return func() int64 {
		lock.Lock()
		defer lock.Unlock()
		if !valid.IsSet() {
			valid = getValidityFlag()
			option, valueCache = getValueCache(name, option, OptTypeInt)
			if valueCache != nil {
				value = valueCache.intVal
			} else {
				value = fallback
			}
		}
		return value
	}
}

// GetAsBool returns a function that returns the wanted int with high performance.
func (cs *safe) GetAsBool(name string, fallback bool) BoolOption {
	valid := getValidityFlag()
	option, valueCache := getValueCache(name, nil, OptTypeBool)
	value := fallback
	if valueCache != nil {
		value = valueCache.boolVal
	}
	var lock sync.Mutex

	return func() bool {
		lock.Lock()
		defer lock.Unlock()
		if !valid.IsSet() {
			valid = getValidityFlag()
			option, valueCache = getValueCache(name, option, OptTypeBool)
			if valueCache != nil {
				value = valueCache.boolVal
			} else {
				value = fallback
			}
		}
		return value
	}
}
