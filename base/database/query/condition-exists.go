package query

import (
	"errors"
	"fmt"

	"github.com/safing/portmaster/base/database/accessor"
)

type existsCondition struct {
	key      string
	operator uint8
}

func newExistsCondition(key string, operator uint8) *existsCondition {
	return &existsCondition{
		key:      key,
		operator: operator,
	}
}

func (c *existsCondition) complies(acc accessor.Accessor) bool {
	return acc.Exists(c.key)
}

func (c *existsCondition) check() error {
	if c.operator == errorPresent {
		return errors.New(c.key)
	}
	return nil
}

func (c *existsCondition) string() string {
	return fmt.Sprintf("%s %s", escapeString(c.key), getOpName(c.operator))
}
