package query

import (
	"fmt"
	"strings"

	"github.com/safing/portmaster/base/database/accessor"
)

// And combines multiple conditions with a logical _AND_ operator.
func And(conditions ...Condition) Condition {
	return &andCond{
		conditions: conditions,
	}
}

type andCond struct {
	conditions []Condition
}

func (c *andCond) complies(acc accessor.Accessor) bool {
	for _, cond := range c.conditions {
		if !cond.complies(acc) {
			return false
		}
	}
	return true
}

func (c *andCond) check() (err error) {
	for _, cond := range c.conditions {
		err = cond.check()
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *andCond) string() string {
	all := make([]string, 0, len(c.conditions))
	for _, cond := range c.conditions {
		all = append(all, cond.string())
	}
	return fmt.Sprintf("(%s)", strings.Join(all, " and "))
}
