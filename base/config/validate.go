package config

import (
	"errors"
	"fmt"
	"math"
	"reflect"

	"github.com/safing/portmaster/base/log"
)

type valueCache struct {
	stringVal      string
	stringArrayVal []string
	intVal         int64
	boolVal        bool
}

func (vc *valueCache) getData(opt *Option) interface{} {
	switch opt.OptType {
	case OptTypeBool:
		return vc.boolVal
	case OptTypeInt:
		return vc.intVal
	case OptTypeString:
		return vc.stringVal
	case OptTypeStringArray:
		return vc.stringArrayVal
	case optTypeAny:
		return nil
	default:
		return nil
	}
}

// isAllowedPossibleValue checks if value is defined as a PossibleValue
// in opt. If there are not possible values defined value is considered
// allowed and nil is returned. isAllowedPossibleValue ensure the actual
// value is an allowed primitiv value by using reflection to convert
// value and each PossibleValue to a comparable primitiv if possible.
// In case of complex value types isAllowedPossibleValue uses
// reflect.DeepEqual as a fallback.
func isAllowedPossibleValue(opt *Option, value interface{}) error {
	if opt.PossibleValues == nil {
		return nil
	}

	for _, val := range opt.PossibleValues {
		compareAgainst := val.Value
		valueType := reflect.TypeOf(value)

		// loading int's from the configuration JSON does not preserve the correct type
		// as we get float64 instead. Make sure to convert them before.
		if reflect.TypeOf(val.Value).ConvertibleTo(valueType) {
			compareAgainst = reflect.ValueOf(val.Value).Convert(valueType).Interface()
		}
		if compareAgainst == value {
			return nil
		}

		if reflect.DeepEqual(val.Value, value) {
			return nil
		}
	}

	return errors.New("value is not allowed")
}

// migrateValue runs all value migrations.
func migrateValue(option *Option, value any) any {
	for _, migration := range option.Migrations {
		newValue := migration(option, value)
		if newValue != value {
			log.Debugf("config: migrated %s value from %v to %v", option.Key, value, newValue)
		}
		value = newValue
	}
	return value
}

// validateValue ensures that value matches the expected type of option.
// It does not create a copy of the value!
func validateValue(option *Option, value interface{}) (*valueCache, *ValidationError) { //nolint:gocyclo
	if option.OptType != OptTypeStringArray {
		if err := isAllowedPossibleValue(option, value); err != nil {
			return nil, &ValidationError{
				Option: option.copyOrNil(),
				Err:    err,
			}
		}
	}

	var validated *valueCache
	switch v := value.(type) {
	case string:
		if option.OptType != OptTypeString {
			return nil, invalid(option, "expected type %s, got type %T", getTypeName(option.OptType), v)
		}
		if option.compiledRegex != nil {
			if !option.compiledRegex.MatchString(v) {
				return nil, invalid(option, "did not match validation regex")
			}
		}
		validated = &valueCache{stringVal: v}
	case []interface{}:
		vConverted := make([]string, len(v))
		for pos, entry := range v {
			s, ok := entry.(string)
			if !ok {
				return nil, invalid(option, "entry #%d is not a string", pos+1)
			}
			vConverted[pos] = s
		}
		// Call validation function again with converted value.
		var vErr *ValidationError
		validated, vErr = validateValue(option, vConverted)
		if vErr != nil {
			return nil, vErr
		}
	case []string:
		if option.OptType != OptTypeStringArray {
			return nil, invalid(option, "expected type %s, got type %T", getTypeName(option.OptType), v)
		}
		if option.compiledRegex != nil {
			for pos, entry := range v {
				if !option.compiledRegex.MatchString(entry) {
					return nil, invalid(option, "entry #%d did not match validation regex", pos+1)
				}

				if err := isAllowedPossibleValue(option, entry); err != nil {
					return nil, invalid(option, "entry #%d is not allowed", pos+1)
				}
			}
		}
		validated = &valueCache{stringArrayVal: v}
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, float32, float64:
		// uint64 is omitted, as it does not fit in a int64
		if option.OptType != OptTypeInt {
			return nil, invalid(option, "expected type %s, got type %T", getTypeName(option.OptType), v)
		}
		if option.compiledRegex != nil {
			// we need to use %v here so we handle float and int correctly.
			if !option.compiledRegex.MatchString(fmt.Sprintf("%v", v)) {
				return nil, invalid(option, "did not match validation regex")
			}
		}
		switch v := value.(type) {
		case int:
			validated = &valueCache{intVal: int64(v)}
		case int8:
			validated = &valueCache{intVal: int64(v)}
		case int16:
			validated = &valueCache{intVal: int64(v)}
		case int32:
			validated = &valueCache{intVal: int64(v)}
		case int64:
			validated = &valueCache{intVal: v}
		case uint:
			validated = &valueCache{intVal: int64(v)}
		case uint8:
			validated = &valueCache{intVal: int64(v)}
		case uint16:
			validated = &valueCache{intVal: int64(v)}
		case uint32:
			validated = &valueCache{intVal: int64(v)}
		case float32:
			// convert if float has no decimals
			if math.Remainder(float64(v), 1) == 0 {
				validated = &valueCache{intVal: int64(v)}
			} else {
				return nil, invalid(option, "failed to convert float32 to int64")
			}
		case float64:
			// convert if float has no decimals
			if math.Remainder(v, 1) == 0 {
				validated = &valueCache{intVal: int64(v)}
			} else {
				return nil, invalid(option, "failed to convert float64 to int64")
			}
		default:
			return nil, invalid(option, "internal error")
		}
	case bool:
		if option.OptType != OptTypeBool {
			return nil, invalid(option, "expected type %s, got type %T", getTypeName(option.OptType), v)
		}
		validated = &valueCache{boolVal: v}
	default:
		return nil, invalid(option, "invalid option value type: %T", value)
	}

	// Check if there is an additional function to validate the value.
	if option.ValidationFunc != nil {
		var err error
		switch option.OptType {
		case optTypeAny:
			err = errors.New("internal error")
		case OptTypeString:
			err = option.ValidationFunc(validated.stringVal)
		case OptTypeStringArray:
			err = option.ValidationFunc(validated.stringArrayVal)
		case OptTypeInt:
			err = option.ValidationFunc(validated.intVal)
		case OptTypeBool:
			err = option.ValidationFunc(validated.boolVal)
		}
		if err != nil {
			return nil, &ValidationError{
				Option: option.copyOrNil(),
				Err:    err,
			}
		}
	}

	return validated, nil
}

// ValidationError error holds details about a config option value validation error.
type ValidationError struct {
	Option *Option
	Err    error
}

// Error returns the formatted error.
func (ve *ValidationError) Error() string {
	return fmt.Sprintf("validation of %s failed: %s", ve.Option.Key, ve.Err)
}

// Unwrap returns the wrapped error.
func (ve *ValidationError) Unwrap() error {
	return ve.Err
}

func invalid(option *Option, format string, a ...interface{}) *ValidationError {
	return &ValidationError{
		Option: option.copyOrNil(),
		Err:    fmt.Errorf(format, a...),
	}
}
