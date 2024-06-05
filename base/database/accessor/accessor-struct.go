package accessor

import (
	"errors"
	"fmt"
	"reflect"
)

// StructAccessor is a json string with get functions.
type StructAccessor struct {
	object reflect.Value
}

// NewStructAccessor adds the Accessor interface to a JSON string.
func NewStructAccessor(object interface{}) *StructAccessor {
	return &StructAccessor{
		object: reflect.ValueOf(object).Elem(),
	}
}

// Set sets the value identified by key.
func (sa *StructAccessor) Set(key string, value interface{}) error {
	field := sa.object.FieldByName(key)
	if !field.IsValid() {
		return errors.New("struct field does not exist")
	}
	if !field.CanSet() {
		return fmt.Errorf("field %s or struct is immutable", field.String())
	}

	newVal := reflect.ValueOf(value)

	// set directly if type matches
	if newVal.Kind() == field.Kind() {
		field.Set(newVal)
		return nil
	}

	// handle special cases
	switch field.Kind() { // nolint:exhaustive

	// ints
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		var newInt int64
		switch newVal.Kind() { // nolint:exhaustive
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			newInt = newVal.Int()
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			newInt = int64(newVal.Uint())
		default:
			return fmt.Errorf("tried to set field %s (%s) to a %s value", key, field.Kind().String(), newVal.Kind().String())
		}
		if field.OverflowInt(newInt) {
			return fmt.Errorf("setting field %s (%s) to %d would overflow", key, field.Kind().String(), newInt)
		}
		field.SetInt(newInt)

		// uints
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		var newUint uint64
		switch newVal.Kind() { // nolint:exhaustive
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			newUint = uint64(newVal.Int())
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			newUint = newVal.Uint()
		default:
			return fmt.Errorf("tried to set field %s (%s) to a %s value", key, field.Kind().String(), newVal.Kind().String())
		}
		if field.OverflowUint(newUint) {
			return fmt.Errorf("setting field %s (%s) to %d would overflow", key, field.Kind().String(), newUint)
		}
		field.SetUint(newUint)

		// floats
	case reflect.Float32, reflect.Float64:
		switch newVal.Kind() { // nolint:exhaustive
		case reflect.Float32, reflect.Float64:
			field.SetFloat(newVal.Float())
		default:
			return fmt.Errorf("tried to set field %s (%s) to a %s value", key, field.Kind().String(), newVal.Kind().String())
		}
	default:
		return fmt.Errorf("tried to set field %s (%s) to a %s value", key, field.Kind().String(), newVal.Kind().String())
	}

	return nil
}

// Get returns the value found by the given json key and whether it could be successfully extracted.
func (sa *StructAccessor) Get(key string) (value interface{}, ok bool) {
	field := sa.object.FieldByName(key)
	if !field.IsValid() || !field.CanInterface() {
		return nil, false
	}
	return field.Interface(), true
}

// GetString returns the string found by the given json key and whether it could be successfully extracted.
func (sa *StructAccessor) GetString(key string) (value string, ok bool) {
	field := sa.object.FieldByName(key)
	if !field.IsValid() || field.Kind() != reflect.String {
		return "", false
	}
	return field.String(), true
}

// GetStringArray returns the []string found by the given json key and whether it could be successfully extracted.
func (sa *StructAccessor) GetStringArray(key string) (value []string, ok bool) {
	field := sa.object.FieldByName(key)
	if !field.IsValid() || field.Kind() != reflect.Slice || !field.CanInterface() {
		return nil, false
	}
	v := field.Interface()
	slice, ok := v.([]string)
	if !ok {
		return nil, false
	}
	return slice, true
}

// GetInt returns the int found by the given json key and whether it could be successfully extracted.
func (sa *StructAccessor) GetInt(key string) (value int64, ok bool) {
	field := sa.object.FieldByName(key)
	if !field.IsValid() {
		return 0, false
	}
	switch field.Kind() { // nolint:exhaustive
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return field.Int(), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return int64(field.Uint()), true
	default:
		return 0, false
	}
}

// GetFloat returns the float found by the given json key and whether it could be successfully extracted.
func (sa *StructAccessor) GetFloat(key string) (value float64, ok bool) {
	field := sa.object.FieldByName(key)
	if !field.IsValid() {
		return 0, false
	}
	switch field.Kind() { // nolint:exhaustive
	case reflect.Float32, reflect.Float64:
		return field.Float(), true
	default:
		return 0, false
	}
}

// GetBool returns the bool found by the given json key and whether it could be successfully extracted.
func (sa *StructAccessor) GetBool(key string) (value bool, ok bool) {
	field := sa.object.FieldByName(key)
	if !field.IsValid() || field.Kind() != reflect.Bool {
		return false, false
	}
	return field.Bool(), true
}

// Exists returns the whether the given key exists.
func (sa *StructAccessor) Exists(key string) bool {
	field := sa.object.FieldByName(key)
	return field.IsValid()
}

// Type returns the accessor type as a string.
func (sa *StructAccessor) Type() string {
	return "StructAccessor"
}
