package accessor

import (
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// JSONBytesAccessor is a json string with get functions.
type JSONBytesAccessor struct {
	json *[]byte
}

// NewJSONBytesAccessor adds the Accessor interface to a JSON bytes string.
func NewJSONBytesAccessor(json *[]byte) *JSONBytesAccessor {
	return &JSONBytesAccessor{
		json: json,
	}
}

// Set sets the value identified by key.
func (ja *JSONBytesAccessor) Set(key string, value interface{}) error {
	result := gjson.GetBytes(*ja.json, key)
	if result.Exists() {
		err := checkJSONValueType(result, key, value)
		if err != nil {
			return err
		}
	}

	newJSON, err := sjson.SetBytes(*ja.json, key, value)
	if err != nil {
		return err
	}
	*ja.json = newJSON
	return nil
}

// Get returns the value found by the given json key and whether it could be successfully extracted.
func (ja *JSONBytesAccessor) Get(key string) (value interface{}, ok bool) {
	result := gjson.GetBytes(*ja.json, key)
	if !result.Exists() {
		return nil, false
	}
	return result.Value(), true
}

// GetString returns the string found by the given json key and whether it could be successfully extracted.
func (ja *JSONBytesAccessor) GetString(key string) (value string, ok bool) {
	result := gjson.GetBytes(*ja.json, key)
	if !result.Exists() || result.Type != gjson.String {
		return emptyString, false
	}
	return result.String(), true
}

// GetStringArray returns the []string found by the given json key and whether it could be successfully extracted.
func (ja *JSONBytesAccessor) GetStringArray(key string) (value []string, ok bool) {
	result := gjson.GetBytes(*ja.json, key)
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
func (ja *JSONBytesAccessor) GetInt(key string) (value int64, ok bool) {
	result := gjson.GetBytes(*ja.json, key)
	if !result.Exists() || result.Type != gjson.Number {
		return 0, false
	}
	return result.Int(), true
}

// GetFloat returns the float found by the given json key and whether it could be successfully extracted.
func (ja *JSONBytesAccessor) GetFloat(key string) (value float64, ok bool) {
	result := gjson.GetBytes(*ja.json, key)
	if !result.Exists() || result.Type != gjson.Number {
		return 0, false
	}
	return result.Float(), true
}

// GetBool returns the bool found by the given json key and whether it could be successfully extracted.
func (ja *JSONBytesAccessor) GetBool(key string) (value bool, ok bool) {
	result := gjson.GetBytes(*ja.json, key)
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
func (ja *JSONBytesAccessor) Exists(key string) bool {
	result := gjson.GetBytes(*ja.json, key)
	return result.Exists()
}

// Type returns the accessor type as a string.
func (ja *JSONBytesAccessor) Type() string {
	return "JSONBytesAccessor"
}
