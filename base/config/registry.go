package config

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
)

var (
	optionsLock sync.RWMutex
	options     = make(map[string]*Option)
)

// ForEachOption calls fn for each defined option. If fn returns
// and error the iteration is stopped and the error is returned.
// Note that ForEachOption does not guarantee a stable order of
// iteration between multiple calles. ForEachOption does NOT lock
// opt when calling fn.
func ForEachOption(fn func(opt *Option) error) error {
	optionsLock.RLock()
	defer optionsLock.RUnlock()

	for _, opt := range options {
		if err := fn(opt); err != nil {
			return err
		}
	}
	return nil
}

// ExportOptions exports the registered options. The returned data must be
// treated as immutable.
// The data does not include the current active or default settings.
func ExportOptions() []*Option {
	optionsLock.RLock()
	defer optionsLock.RUnlock()

	// Copy the map into a slice.
	opts := make([]*Option, 0, len(options))
	for _, opt := range options {
		opts = append(opts, opt)
	}

	sort.Sort(sortByKey(opts))
	return opts
}

// GetOption returns the option with name or an error
// if the option does not exist. The caller should lock
// the returned option itself for further processing.
func GetOption(name string) (*Option, error) {
	optionsLock.RLock()
	defer optionsLock.RUnlock()

	opt, ok := options[name]
	if !ok {
		return nil, fmt.Errorf("option %q does not exist", name)
	}
	return opt, nil
}

// Register registers a new configuration option.
func Register(option *Option) error {
	if option.Name == "" {
		return fmt.Errorf("failed to register option: please set option.Name")
	}
	if option.Key == "" {
		return fmt.Errorf("failed to register option: please set option.Key")
	}
	if option.Description == "" {
		return fmt.Errorf("failed to register option: please set option.Description")
	}
	if option.OptType == 0 {
		return fmt.Errorf("failed to register option: please set option.OptType")
	}

	if option.ValidationRegex == "" && option.PossibleValues != nil {
		values := make([]string, len(option.PossibleValues))
		for idx, val := range option.PossibleValues {
			values[idx] = fmt.Sprintf("%v", val.Value)
		}
		option.ValidationRegex = fmt.Sprintf("^(%s)$", strings.Join(values, "|"))
	}

	var err error
	if option.ValidationRegex != "" {
		option.compiledRegex, err = regexp.Compile(option.ValidationRegex)
		if err != nil {
			return fmt.Errorf("config: could not compile option.ValidationRegex: %w", err)
		}
	}

	var vErr *ValidationError
	option.activeFallbackValue, vErr = validateValue(option, option.DefaultValue)
	if vErr != nil {
		return fmt.Errorf("config: invalid default value: %w", vErr)
	}

	optionsLock.Lock()
	defer optionsLock.Unlock()
	options[option.Key] = option

	return nil
}
