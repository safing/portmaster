package query

import (
	"fmt"
	"strings"

	"github.com/safing/portmaster/base/database/accessor"
)

// Or combines multiple conditions with a logical _OR_ operator.
func Or(conditions ...Condition) Condition {
	return &orCond{
		conditions: conditions,
	}
}

type orCond struct {
	conditions []Condition
}

func (c *orCond) complies(acc accessor.Accessor) bool {
	for _, cond := range c.conditions {
		if cond.complies(acc) {
			return true
		}
	}
	return false
}

func (c *orCond) check() (err error) {
	for _, cond := range c.conditions {
		err = cond.check()
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *orCond) string() string {
	all := make([]string, 0, len(c.conditions))
	for _, cond := range c.conditions {
		all = append(all, cond.string())
	}
	return fmt.Sprintf("(%s)", strings.Join(all, " or "))
}
