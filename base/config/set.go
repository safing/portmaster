package config

import (
	"errors"
	"sync"

	"github.com/tevino/abool"
)

var (
	// ErrInvalidJSON is returned by SetConfig and SetDefaultConfig if they receive invalid json.
	ErrInvalidJSON = errors.New("json string invalid")

	// ErrInvalidOptionType is returned by SetConfigOption and SetDefaultConfigOption if given an unsupported option type.
	ErrInvalidOptionType = errors.New("invalid option value type")

	validityFlag     = abool.NewBool(true)
	validityFlagLock sync.RWMutex
)

// getValidityFlag returns a flag that signifies if the configuration has been changed. This flag must not be changed, only read.
func getValidityFlag() *abool.AtomicBool {
	validityFlagLock.RLock()
	defer validityFlagLock.RUnlock()
	return validityFlag
}

// signalChanges marks the configs validtityFlag as dirty and eventually
// triggers a config change event.
func signalChanges() {
	// reset validity flag
	validityFlagLock.Lock()
	validityFlag.SetTo(false)
	validityFlag = abool.NewBool(true)
	validityFlagLock.Unlock()

	module.EventConfigChange.Submit(struct{}{})
}

// ValidateConfig validates the given configuration and returns all validation
// errors as well as whether the given configuration contains unknown keys.
func ValidateConfig(newValues map[string]interface{}) (validationErrors []*ValidationError, requiresRestart bool, containsUnknown bool) {
	// RLock the options because we are not adding or removing
	// options from the registration but rather only checking the
	// options value which is guarded by the option's lock itself.
	optionsLock.RLock()
	defer optionsLock.RUnlock()

	var checked int
	for key, option := range options {
		newValue, ok := newValues[key]
		if ok {
			checked++

			func() {
				option.Lock()
				defer option.Unlock()

				newValue = migrateValue(option, newValue)
				_, err := validateValue(option, newValue)
				if err != nil {
					validationErrors = append(validationErrors, err)
				}

				if option.RequiresRestart {
					requiresRestart = true
				}
			}()
		}
	}

	return validationErrors, requiresRestart, checked < len(newValues)
}

// ReplaceConfig sets the (prioritized) user defined config.
func ReplaceConfig(newValues map[string]interface{}) (validationErrors []*ValidationError, requiresRestart bool) {
	// RLock the options because we are not adding or removing
	// options from the registration but rather only update the
	// options value which is guarded by the option's lock itself.
	optionsLock.RLock()
	defer optionsLock.RUnlock()

	for key, option := range options {
		newValue, ok := newValues[key]

		func() {
			option.Lock()
			defer option.Unlock()

			option.activeValue = nil
			if ok {
				newValue = migrateValue(option, newValue)
				valueCache, err := validateValue(option, newValue)
				if err == nil {
					option.activeValue = valueCache
				} else {
					validationErrors = append(validationErrors, err)
				}
			}
			handleOptionUpdate(option, true)

			if option.RequiresRestart {
				requiresRestart = true
			}
		}()
	}

	signalChanges()

	return validationErrors, requiresRestart
}

// ReplaceDefaultConfig sets the (fallback) default config.
func ReplaceDefaultConfig(newValues map[string]interface{}) (validationErrors []*ValidationError, requiresRestart bool) {
	// RLock the options because we are not adding or removing
	// options from the registration but rather only update the
	// options value which is guarded by the option's lock itself.
	optionsLock.RLock()
	defer optionsLock.RUnlock()

	for key, option := range options {
		newValue, ok := newValues[key]

		func() {
			option.Lock()
			defer option.Unlock()

			option.activeDefaultValue = nil
			if ok {
				newValue = migrateValue(option, newValue)
				valueCache, err := validateValue(option, newValue)
				if err == nil {
					option.activeDefaultValue = valueCache
				} else {
					validationErrors = append(validationErrors, err)
				}
			}
			handleOptionUpdate(option, true)

			if option.RequiresRestart {
				requiresRestart = true
			}
		}()
	}

	signalChanges()

	return validationErrors, requiresRestart
}

// SetConfigOption sets a single value in the (prioritized) user defined config.
func SetConfigOption(key string, value any) error {
	return setConfigOption(key, value, true)
}

func setConfigOption(key string, value any, push bool) (err error) {
	option, err := GetOption(key)
	if err != nil {
		return err
	}

	option.Lock()
	if value == nil {
		option.activeValue = nil
	} else {
		value = migrateValue(option, value)
		valueCache, vErr := validateValue(option, value)
		if vErr == nil {
			option.activeValue = valueCache
		} else {
			err = vErr
		}
	}

	// Add the "restart pending" annotation if the settings requires a restart.
	if option.RequiresRestart {
		option.setAnnotation(RestartPendingAnnotation, true)
	}

	handleOptionUpdate(option, push)
	option.Unlock()

	if err != nil {
		return err
	}

	// finalize change, activate triggers
	signalChanges()

	return SaveConfig()
}

// SetDefaultConfigOption sets a single value in the (fallback) default config.
func SetDefaultConfigOption(key string, value interface{}) error {
	return setDefaultConfigOption(key, value, true)
}

func setDefaultConfigOption(key string, value interface{}, push bool) (err error) {
	option, err := GetOption(key)
	if err != nil {
		return err
	}

	option.Lock()
	if value == nil {
		option.activeDefaultValue = nil
	} else {
		value = migrateValue(option, value)
		valueCache, vErr := validateValue(option, value)
		if vErr == nil {
			option.activeDefaultValue = valueCache
		} else {
			err = vErr
		}
	}

	// Add the "restart pending" annotation if the settings requires a restart.
	if option.RequiresRestart {
		option.setAnnotation(RestartPendingAnnotation, true)
	}

	handleOptionUpdate(option, push)
	option.Unlock()

	if err != nil {
		return err
	}

	// finalize change, activate triggers
	signalChanges()

	// Do not save the configuration, as it only saves the active values, not the
	// active default value.
	return nil
}
