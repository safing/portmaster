package query

import (
	"fmt"
	"strings"

	"github.com/safing/portmaster/base/database/accessor"
)

// Not negates the supplied condition.
func Not(c Condition) Condition {
	return &notCond{
		notC: c,
	}
}

type notCond struct {
	notC Condition
}

func (c *notCond) complies(acc accessor.Accessor) bool {
	return !c.notC.complies(acc)
}

func (c *notCond) check() error {
	return c.notC.check()
}

func (c *notCond) string() string {
	next := c.notC.string()
	if strings.HasPrefix(next, "(") {
		return fmt.Sprintf("not %s", c.notC.string())
	}
	splitted := strings.Split(next, " ")
	return strings.Join(append([]string{splitted[0], "not"}, splitted[1:]...), " ")
}
