package accessor

import (
	"fmt"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// JSONAccessor is a json string with get functions.
type JSONAccessor struct {
	json *string
}

// NewJSONAccessor adds the Accessor interface to a JSON string.
func NewJSONAccessor(json *string) *JSONAccessor {
	return &JSONAccessor{
		json: json,
	}
}

// Set sets the value identified by key.
func (ja *JSONAccessor) Set(key string, value interface{}) error {
	result := gjson.Get(*ja.json, key)
	if result.Exists() {
		err := checkJSONValueType(result, key, value)
		if err != nil {
			return err
		}
	}

	newJSON, err := sjson.Set(*ja.json, key, value)
	if err != nil {
		return err
	}
	*ja.json = newJSON
	return nil
}

func checkJSONValueType(jsonValue gjson.Result, key string, value interface{}) error {
	switch value.(type) {
	case string:
		if jsonValue.Type != gjson.String {
			return fmt.Errorf("tried to set field %s (%s) to a %T value", key, jsonValue.Type.String(), value)
		}
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		if jsonValue.Type != gjson.Number {
			return fmt.Errorf("tried to set field %s (%s) to a %T value", key, jsonValue.Type.String(), value)
		}
	case bool:
		if jsonValue.Type != gjson.True && jsonValue.Type != gjson.False {
			return fmt.Errorf("tried to set field %s (%s) to a %T value", key, jsonValue.Type.String(), value)
		}
	case []string:
		if !jsonValue.IsArray() {
			return fmt.Errorf("tried to set field %s (%s) to a %T value", key, jsonValue.Type.String(), value)
		}
	}
	return nil
}

// Get returns the value found by the given json key and whether it could be successfully extracted.
func (ja *JSONAccessor) Get(key string) (value interface{}, ok bool) {
	result := gjson.Get(*ja.json, key)
	if !result.Exists() {
		return nil, false
	}
	return result.Value(), true
}

// GetString returns the string found by the given json key and whether it could be successfully extracted.
func (ja *JSONAccessor) GetString(key string) (value string, ok bool) {
	result := gjson.Get(*ja.json, key)
	if !result.Exists() || result.Type != gjson.String {
		return emptyString, false
	}
	return result.String(), true
}

// GetStringArray returns the []string found by the given json key and whether it could be successfully extracted.
func (ja *JSONAccessor) GetStringArray(key string) (value []string, ok bool) {
	result := gjson.Get(*ja.json, key)
	if !result.Exists() && !result.IsArray() {
		return nil, false
	}
	slice := result.Array()
	sliceCopy := make([]string, len(slice))
	for i, res := range slice {
		if res.Type == gjson.String {
			sliceCopy[i] = res.String()
		} else {
			return nil, false
		}
	}
	return sliceCopy, true
}

// GetInt returns the int found by the given json key and whether it could be successfully extracted.
func (ja *JSONAccessor) GetInt(key string) (value int64, ok bool) {
	result := gjson.Get(*ja.json, key)
	if !result.Exists() || result.Type != gjson.Number {
		return 0, false
	}
	return result.Int(), true
}

// GetFloat returns the float found by the given json key and whether it could be successfully extracted.
func (ja *JSONAccessor) GetFloat(key string) (value float64, ok bool) {
	result := gjson.Get(*ja.json, key)
	if !result.Exists() || result.Type != gjson.Number {
		return 0, false
	}
	return result.Float(), true
}

// GetBool returns the bool found by the given json key and whether it could be successfully extracted.
func (ja *JSONAccessor) GetBool(key string) (value bool, ok bool) {
	result := gjson.Get(*ja.json, key)
	switch {
	case !result.Exists():
		return false, false
	case result.Type == gjson.True:
		return true, true
	case result.Type == gjson.False:
		return false, true
	default:
		return false, false
	}
}

// Exists returns the whether the given key exists.
func (ja *JSONAccessor) Exists(key string) bool {
	result := gjson.Get(*ja.json, key)
	return result.Exists()
}

// Type returns the accessor type as a string.
func (ja *JSONAccessor) Type() string {
	return "JSONAccessor"
}
