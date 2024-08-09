package query

import (
	"errors"
	"fmt"
	"regexp"

	"github.com/safing/portmaster/base/database/accessor"
)

type regexCondition struct {
	key      string
	operator uint8
	regex    *regexp.Regexp
}

func newRegexCondition(key string, operator uint8, value interface{}) *regexCondition {
	switch v := value.(type) {
	case string:
		r, err := regexp.Compile(v)
		if err != nil {
			return &regexCondition{
				key:      fmt.Sprintf("could not compile regex \"%s\": %s", v, err),
				operator: errorPresent,
			}
		}
		return &regexCondition{
			key:      key,
			operator: operator,
			regex:    r,
		}
	default:
		return &regexCondition{
			key:      fmt.Sprintf("incompatible value %v for string", value),
			operator: errorPresent,
		}
	}
}

func (c *regexCondition) complies(acc accessor.Accessor) bool {
	comp, ok := acc.GetString(c.key)
	if !ok {
		return false
	}

	switch c.operator {
	case Matches:
		return c.regex.MatchString(comp)
	default:
		return false
	}
}

func (c *regexCondition) check() error {
	if c.operator == errorPresent {
		return errors.New(c.key)
	}
	return nil
}

func (c *regexCondition) string() string {
	return fmt.Sprintf("%s %s %s", escapeString(c.key), getOpName(c.operator), escapeString(c.regex.String()))
}
