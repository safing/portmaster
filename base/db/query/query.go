package query

import (
	"fmt"
	"strings"

	"github.com/safing/portmaster/base/db/accessor"
)

// Example:
// q.New("core:/",
//   q.Where("a", q.GreaterThan, 0),
//   q.Where("b", q.Equals, 0),
//   q.Or(
//       q.Where("c", q.StartsWith, "x"),
//       q.Where("d", q.Contains, "y")
//     )
//   )

// Query contains a compiled query.
type Query struct {
	checked    bool
	keyPrefix  string
	where      Condition
	orderBy    string
	limit      int
	offset     int
	accessPerm int8
}

type Record interface {
	Key() string
	Permission() int8
	GetAccessor() accessor.Accessor
}

// New creates a new query with the supplied prefix.
func New(prefix string) *Query {
	return &Query{
		keyPrefix: prefix,
	}
}

// Where adds filtering.
func (q *Query) Where(condition Condition) *Query {
	q.where = condition
	return q
}

// Limit limits the number of returned results.
func (q *Query) Limit(limit int) *Query {
	q.limit = limit
	return q
}

// Offset sets the query offset.
func (q *Query) Offset(offset int) *Query {
	q.offset = offset
	return q
}

// OrderBy orders the results by the given key.
func (q *Query) OrderBy(key string) *Query {
	q.orderBy = key
	return q
}

// SetAccessPermission sets the access permission on the query.
// The given permission must be regular, ie. not negative.
func (q *Query) SetAccessPermission(permission int8) *Query {
	if permission >= 0 {
		q.accessPerm = permission
	}
	return q
}

// Check checks for errors in the query.
func (q *Query) Check() error {
	if q.checked {
		return nil
	}

	// check condition
	if q.where != nil {
		err := q.where.check()
		if err != nil {
			return err
		}
	}

	q.checked = true
	return nil
}

// MustBeValid checks for errors in the query and panics if there is an error.
func (q *Query) MustBeValid() *Query {
	if err := q.Check(); err != nil {
		panic(err)
	}
	return q
}

// IsChecked returns whether they query was checked.
func (q *Query) IsChecked() bool {
	return q.checked
}

// MatchesKey checks whether the query matches the supplied database key (key without database prefix).
func (q *Query) MatchesKey(key string) bool {
	return strings.HasPrefix(key, q.keyPrefix)
}

// MatchesPermission checks whether the query is allowed to access the record.
func (q *Query) MatchesPermission(accessPermission int8) bool {
	return q.accessPerm <= accessPermission
}

// MatchesRecord checks whether the query matches the supplied database record (value only).
func (q *Query) MatchesRecord(r Record) bool {
	if q.where == nil {
		return true
	}

	acc := r.GetAccessor()
	if acc == nil {
		return false
	}
	return q.where.complies(acc)
}

// MatchesAccessor checks whether the query matches the supplied accessor (value only).
func (q *Query) MatchesAccessor(acc accessor.Accessor) bool {
	if q.where == nil {
		return true
	}
	return q.where.complies(acc)
}

// Matches checks whether the query matches the supplied database record.
func (q *Query) Matches(r Record) bool {
	if !q.MatchesPermission(r.Permission()) {
		return false
	}
	if !q.MatchesKey(r.Key()) {
		return false
	}
	return q.MatchesRecord(r)
}

// Print returns the string representation of the query.
func (q *Query) Print() string {
	var where string
	if q.where != nil {
		where = q.where.string()
		if where != "" {
			if strings.HasPrefix(where, "(") {
				where = where[1 : len(where)-1]
			}
			where = fmt.Sprintf(" where %s", where)
		}
	}

	var orderBy string
	if q.orderBy != "" {
		orderBy = fmt.Sprintf(" orderby %s", q.orderBy)
	}

	var limit string
	if q.limit > 0 {
		limit = fmt.Sprintf(" limit %d", q.limit)
	}

	var offset string
	if q.offset > 0 {
		offset = fmt.Sprintf(" offset %d", q.offset)
	}

	return fmt.Sprintf("query %s%s%s%s%s", q.keyPrefix, where, orderBy, limit, offset)
}

// KeyPrefix returns the key prefix of the query.
func (q *Query) KeyPrefix() string {
	return q.keyPrefix
}
