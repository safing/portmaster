package intel

import (
	"fmt"
	"strings"
)

// ListMatch represents an entity that has been
// matched against filterlists.
type ListMatch struct {
	Entity        string
	ActiveLists   []string
	InactiveLists []string
}

func (lm *ListMatch) String() string {
	inactive := ""
	if len(lm.InactiveLists) > 0 {
		inactive = " and in deactivated lists " + strings.Join(lm.InactiveLists, ", ")
	}
	return fmt.Sprintf(
		"%s in activated lists %s%s",
		lm.Entity,
		strings.Join(lm.ActiveLists, ","),
		inactive,
	)
}

// ListBlockReason is a list of list matches.
type ListBlockReason []ListMatch

func (br ListBlockReason) String() string {
	if len(br) == 0 {
		return ""
	}

	matches := make([]string, len(br))
	for idx, lm := range br {
		matches[idx] = lm.String()
	}

	return strings.Join(matches, " and ")
}

// Context returns br wrapped into a map. It implements
// the endpoints.Reason interface.
func (br ListBlockReason) Context() interface{} {
	return map[string]interface{}{
		"filterlists": br,
	}
}
