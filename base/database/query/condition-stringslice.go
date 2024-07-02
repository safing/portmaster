package query

import (
	"fmt"
	"strings"

	"github.com/safing/portmaster/base/database/accessor"
	"github.com/safing/portmaster/base/utils"
)

type stringSliceCondition struct {
	key      string
	operator uint8
	value    []string
}

func newStringSliceCondition(key string, operator uint8, value interface{}) *stringSliceCondition {
	switch v := value.(type) {
	case string:
		parsedValue := strings.Split(v, ",")
		if len(parsedValue) < 2 {
			return &stringSliceCondition{
				key:      v,
				operator: errorPresent,
			}
		}
		return &stringSliceCondition{
			key:      key,
			operator: operator,
			value:    parsedValue,
		}
	case []string:
		return &stringSliceCondition{
			key:      key,
			operator: operator,
			value:    v,
		}
	default:
		return &stringSliceCondition{
			key:      fmt.Sprintf("incompatible value %v for []string", value),
			operator: errorPresent,
		}
	}
}

func (c *stringSliceCondition) complies(acc accessor.Accessor) bool {
	comp, ok := acc.GetString(c.key)
	if !ok {
		return false
	}

	switch c.operator {
	case In:
		return utils.StringInSlice(c.value, comp)
	default:
		return false
	}
}

func (c *stringSliceCondition) check() error {
	if c.operator == errorPresent {
		return fmt.Errorf("could not parse \"%s\" to []string", c.key)
	}
	return nil
}

func (c *stringSliceCondition) string() string {
	return fmt.Sprintf("%s %s %s", escapeString(c.key), getOpName(c.operator), escapeString(strings.Join(c.value, ",")))
}
