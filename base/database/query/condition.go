package query

import (
	"fmt"

	"github.com/safing/portmaster/base/database/accessor"
)

// Condition is an interface to provide a common api to all condition types.
type Condition interface {
	complies(acc accessor.Accessor) bool
	check() error
	string() string
}

// Operators.
const (
	Equals                  uint8 = iota // int
	GreaterThan                          // int
	GreaterThanOrEqual                   // int
	LessThan                             // int
	LessThanOrEqual                      // int
	FloatEquals                          // float
	FloatGreaterThan                     // float
	FloatGreaterThanOrEqual              // float
	FloatLessThan                        // float
	FloatLessThanOrEqual                 // float
	SameAs                               // string
	Contains                             // string
	StartsWith                           // string
	EndsWith                             // string
	In                                   // stringSlice
	Matches                              // regex
	Is                                   // bool: accepts 1, t, T, TRUE, true, True, 0, f, F, FALSE
	Exists                               // any

	errorPresent uint8 = 255
)

// Where returns a condition to add to a query.
func Where(key string, operator uint8, value interface{}) Condition {
	switch operator {
	case Equals,
		GreaterThan,
		GreaterThanOrEqual,
		LessThan,
		LessThanOrEqual:
		return newIntCondition(key, operator, value)
	case FloatEquals,
		FloatGreaterThan,
		FloatGreaterThanOrEqual,
		FloatLessThan,
		FloatLessThanOrEqual:
		return newFloatCondition(key, operator, value)
	case SameAs,
		Contains,
		StartsWith,
		EndsWith:
		return newStringCondition(key, operator, value)
	case In:
		return newStringSliceCondition(key, operator, value)
	case Matches:
		return newRegexCondition(key, operator, value)
	case Is:
		return newBoolCondition(key, operator, value)
	case Exists:
		return newExistsCondition(key, operator)
	default:
		return newErrorCondition(fmt.Errorf("no operator with ID %d", operator))
	}
}
