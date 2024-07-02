package query

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/safing/portmaster/base/database/accessor"
)

type intCondition struct {
	key      string
	operator uint8
	value    int64
}

func newIntCondition(key string, operator uint8, value interface{}) *intCondition {
	var parsedValue int64

	switch v := value.(type) {
	case int:
		parsedValue = int64(v)
	case int8:
		parsedValue = int64(v)
	case int16:
		parsedValue = int64(v)
	case int32:
		parsedValue = int64(v)
	case int64:
		parsedValue = v
	case uint:
		parsedValue = int64(v)
	case uint8:
		parsedValue = int64(v)
	case uint16:
		parsedValue = int64(v)
	case uint32:
		parsedValue = int64(v)
	case string:
		var err error
		parsedValue, err = strconv.ParseInt(v, 10, 64)
		if err != nil {
			return &intCondition{
				key:      fmt.Sprintf("could not parse %s to int64: %s (hint: use \"sameas\" to compare strings)", v, err),
				operator: errorPresent,
			}
		}
	default:
		return &intCondition{
			key:      fmt.Sprintf("incompatible value %v for int64", value),
			operator: errorPresent,
		}
	}

	return &intCondition{
		key:      key,
		operator: operator,
		value:    parsedValue,
	}
}

func (c *intCondition) complies(acc accessor.Accessor) bool {
	comp, ok := acc.GetInt(c.key)
	if !ok {
		return false
	}

	switch c.operator {
	case Equals:
		return comp == c.value
	case GreaterThan:
		return comp > c.value
	case GreaterThanOrEqual:
		return comp >= c.value
	case LessThan:
		return comp < c.value
	case LessThanOrEqual:
		return comp <= c.value
	default:
		return false
	}
}

func (c *intCondition) check() error {
	if c.operator == errorPresent {
		return errors.New(c.key)
	}
	return nil
}

func (c *intCondition) string() string {
	return fmt.Sprintf("%s %s %d", escapeString(c.key), getOpName(c.operator), c.value)
}
