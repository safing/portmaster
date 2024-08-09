package query

import (
	"errors"
	"fmt"
	"strings"

	"github.com/safing/portmaster/base/database/accessor"
)

type stringCondition struct {
	key      string
	operator uint8
	value    string
}

func newStringCondition(key string, operator uint8, value interface{}) *stringCondition {
	switch v := value.(type) {
	case string:
		return &stringCondition{
			key:      key,
			operator: operator,
			value:    v,
		}
	default:
		return &stringCondition{
			key:      fmt.Sprintf("incompatible value %v for string", value),
			operator: errorPresent,
		}
	}
}

func (c *stringCondition) complies(acc accessor.Accessor) bool {
	comp, ok := acc.GetString(c.key)
	if !ok {
		return false
	}

	switch c.operator {
	case SameAs:
		return c.value == comp
	case Contains:
		return strings.Contains(comp, c.value)
	case StartsWith:
		return strings.HasPrefix(comp, c.value)
	case EndsWith:
		return strings.HasSuffix(comp, c.value)
	default:
		return false
	}
}

func (c *stringCondition) check() error {
	if c.operator == errorPresent {
		return errors.New(c.key)
	}
	return nil
}

func (c *stringCondition) string() string {
	return fmt.Sprintf("%s %s %s", escapeString(c.key), getOpName(c.operator), escapeString(c.value))
}
