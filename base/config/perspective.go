package config

import (
	"fmt"

	"github.com/safing/portmaster/base/log"
)

// Perspective is a view on configuration data without interfering with the configuration system.
type Perspective struct {
	config map[string]*perspectiveOption
}

type perspectiveOption struct {
	option     *Option
	valueCache *valueCache
}

// NewPerspective parses the given config and returns it as a new perspective.
func NewPerspective(config map[string]interface{}) (*Perspective, error) {
	// flatten config structure
	config = Flatten(config)

	perspective := &Perspective{
		config: make(map[string]*perspectiveOption),
	}
	var firstErr error
	var errCnt int

	optionsLock.RLock()
optionsLoop:
	for key, option := range options {
		// get option key from config
		configValue, ok := config[key]
		if !ok {
			continue
		}
		// migrate value
		configValue = migrateValue(option, configValue)
		// validate value
		valueCache, err := validateValue(option, configValue)
		if err != nil {
			errCnt++
			if firstErr == nil {
				firstErr = err
			}
			continue optionsLoop
		}

		// add to perspective
		perspective.config[key] = &perspectiveOption{
			option:     option,
			valueCache: valueCache,
		}
	}
	optionsLock.RUnlock()

	if firstErr != nil {
		if errCnt > 0 {
			return perspective, fmt.Errorf("encountered %d errors, first was: %w", errCnt, firstErr)
		}
		return perspective, firstErr
	}

	return perspective, nil
}

func (p *Perspective) getPerspectiveValueCache(name string, requestedType OptionType) *valueCache {
	// get option
	pOption, ok := p.config[name]
	if !ok {
		// check if option exists at all
		if _, err := GetOption(name); err != nil {
			log.Errorf("config: request for unregistered option: %s", name)
		}
		return nil
	}

	// check type
	if requestedType != pOption.option.OptType && requestedType != optTypeAny {
		log.Errorf("config: bad type: requested %s as %s, but is %s", name, getTypeName(requestedType), getTypeName(pOption.option.OptType))
		return nil
	}

	// check release level
	if pOption.option.ReleaseLevel > getReleaseLevel() {
		return nil
	}

	return pOption.valueCache
}

// Has returns whether the given option is set in the perspective.
func (p *Perspective) Has(name string) bool {
	valueCache := p.getPerspectiveValueCache(name, optTypeAny)
	return valueCache != nil
}

// GetAsString returns a function that returns the wanted string with high performance.
func (p *Perspective) GetAsString(name string) (value string, ok bool) {
	valueCache := p.getPerspectiveValueCache(name, OptTypeString)
	if valueCache != nil {
		return valueCache.stringVal, true
	}
	return "", false
}

// GetAsStringArray returns a function that returns the wanted string with high performance.
func (p *Perspective) GetAsStringArray(name string) (value []string, ok bool) {
	valueCache := p.getPerspectiveValueCache(name, OptTypeStringArray)
	if valueCache != nil {
		return valueCache.stringArrayVal, true
	}
	return nil, false
}

// GetAsInt returns a function that returns the wanted int with high performance.
func (p *Perspective) GetAsInt(name string) (value int64, ok bool) {
	valueCache := p.getPerspectiveValueCache(name, OptTypeInt)
	if valueCache != nil {
		return valueCache.intVal, true
	}
	return 0, false
}

// GetAsBool returns a function that returns the wanted int with high performance.
func (p *Perspective) GetAsBool(name string) (value bool, ok bool) {
	valueCache := p.getPerspectiveValueCache(name, OptTypeBool)
	if valueCache != nil {
		return valueCache.boolVal, true
	}
	return false, false
}
