package query

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/safing/portmaster/base/database/accessor"
)

type floatCondition struct {
	key      string
	operator uint8
	value    float64
}

func newFloatCondition(key string, operator uint8, value interface{}) *floatCondition {
	var parsedValue float64

	switch v := value.(type) {
	case int:
		parsedValue = float64(v)
	case int8:
		parsedValue = float64(v)
	case int16:
		parsedValue = float64(v)
	case int32:
		parsedValue = float64(v)
	case int64:
		parsedValue = float64(v)
	case uint:
		parsedValue = float64(v)
	case uint8:
		parsedValue = float64(v)
	case uint16:
		parsedValue = float64(v)
	case uint32:
		parsedValue = float64(v)
	case float32:
		parsedValue = float64(v)
	case float64:
		parsedValue = v
	case string:
		var err error
		parsedValue, err = strconv.ParseFloat(v, 64)
		if err != nil {
			return &floatCondition{
				key:      fmt.Sprintf("could not parse %s to float64: %s", v, err),
				operator: errorPresent,
			}
		}
	default:
		return &floatCondition{
			key:      fmt.Sprintf("incompatible value %v for float64", value),
			operator: errorPresent,
		}
	}

	return &floatCondition{
		key:      key,
		operator: operator,
		value:    parsedValue,
	}
}

func (c *floatCondition) complies(acc accessor.Accessor) bool {
	comp, ok := acc.GetFloat(c.key)
	if !ok {
		return false
	}

	switch c.operator {
	case FloatEquals:
		return comp == c.value
	case FloatGreaterThan:
		return comp > c.value
	case FloatGreaterThanOrEqual:
		return comp >= c.value
	case FloatLessThan:
		return comp < c.value
	case FloatLessThanOrEqual:
		return comp <= c.value
	default:
		return false
	}
}

func (c *floatCondition) check() error {
	if c.operator == errorPresent {
		return errors.New(c.key)
	}
	return nil
}

func (c *floatCondition) string() string {
	return fmt.Sprintf("%s %s %g", escapeString(c.key), getOpName(c.operator), c.value)
}
