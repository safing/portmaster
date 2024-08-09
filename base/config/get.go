package config

import (
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

// GetAsString returns a function that returns the wanted string with high performance.
func GetAsString(name string, fallback string) StringOption {
	valid := getValidityFlag()
	option, valueCache := getValueCache(name, nil, OptTypeString)
	value := fallback
	if valueCache != nil {
		value = valueCache.stringVal
	}

	return func() string {
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
func GetAsStringArray(name string, fallback []string) StringArrayOption {
	valid := getValidityFlag()
	option, valueCache := getValueCache(name, nil, OptTypeStringArray)
	value := fallback
	if valueCache != nil {
		value = valueCache.stringArrayVal
	}

	return func() []string {
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
func GetAsInt(name string, fallback int64) IntOption {
	valid := getValidityFlag()
	option, valueCache := getValueCache(name, nil, OptTypeInt)
	value := fallback
	if valueCache != nil {
		value = valueCache.intVal
	}

	return func() int64 {
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
func GetAsBool(name string, fallback bool) BoolOption {
	valid := getValidityFlag()
	option, valueCache := getValueCache(name, nil, OptTypeBool)
	value := fallback
	if valueCache != nil {
		value = valueCache.boolVal
	}

	return func() bool {
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
