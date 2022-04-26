package netquery

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"sort"
	"strings"

	"github.com/hashicorp/go-multierror"
	"github.com/safing/portmaster/netquery/orm"
)

type (
	Query map[string][]Matcher

	Matcher struct {
		Equal    interface{}   `json:"$eq,omitempty"`
		NotEqual interface{}   `json:"$ne,omitempty"`
		In       []interface{} `json:"$in,omitempty"`
		NotIn    []interface{} `json:"$notIn,omitempty"`
		Like     string        `json:"$like,omitempty"`
	}

	Count struct {
		As       string `json:"as"`
		Field    string `json:"field"`
		Distinct bool   `json:"distict"`
	}

	Select struct {
		Field string `json:"field"`
		Count *Count `json:"$count"`
	}

	Selects []Select

	QueryRequestPayload struct {
		Select  Selects   `json:"select"`
		Query   Query     `json:"query"`
		OrderBy []OrderBy `json:"orderBy"`
		GroupBy []string  `json:"groupBy"`

		selectedFields    []string
		whitelistedFields []string
	}

	OrderBy struct {
		Field string `json:"field"`
		Desc  bool   `json:"desc"`
	}

	OrderBys []OrderBy
)

func (query *Query) UnmarshalJSON(blob []byte) error {
	if *query == nil {
		*query = make(Query)
	}

	var model map[string]json.RawMessage

	if err := json.Unmarshal(blob, &model); err != nil {
		return err
	}

	for columnName, rawColumnQuery := range model {
		if len(rawColumnQuery) == 0 {
			continue
		}

		switch rawColumnQuery[0] {
		case '{':
			m, err := parseMatcher(rawColumnQuery)
			if err != nil {
				return err
			}

			(*query)[columnName] = []Matcher{*m}

		case '[':
			var rawMatchers []json.RawMessage
			if err := json.Unmarshal(rawColumnQuery, &rawMatchers); err != nil {
				return err
			}

			(*query)[columnName] = make([]Matcher, len(rawMatchers))
			for idx, val := range rawMatchers {
				// this should not happen
				if len(val) == 0 {
					continue
				}

				// if val starts with a { we have a matcher definition
				if val[0] == '{' {
					m, err := parseMatcher(val)
					if err != nil {
						return err
					}
					(*query)[columnName][idx] = *m

					continue
				} else if val[0] == '[' {
					return fmt.Errorf("invalid token [ in query for column %s", columnName)
				}

				// val is a dedicated JSON primitive and not an object or array
				// so we treat that as an EQUAL condition.
				var x interface{}
				if err := json.Unmarshal(val, &x); err != nil {
					return err
				}

				(*query)[columnName][idx] = Matcher{
					Equal: x,
				}
			}

		default:
			// value is a JSON primitive and not an object or array
			// so we treat that as an EQUAL condition.
			var x interface{}
			if err := json.Unmarshal(rawColumnQuery, &x); err != nil {
				return err
			}

			(*query)[columnName] = []Matcher{
				{Equal: x},
			}
		}
	}

	return nil
}

func parseMatcher(raw json.RawMessage) (*Matcher, error) {
	var m Matcher
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}

	if err := m.Validate(); err != nil {
		return nil, fmt.Errorf("invalid query matcher: %s", err)
	}
	log.Printf("parsed matcher %s: %+v", string(raw), m)
	return &m, nil

}

func (match Matcher) Validate() error {
	found := 0

	if match.Equal != nil {
		found++
	}

	if match.NotEqual != nil {
		found++
	}

	if match.In != nil {
		found++
	}

	if match.NotIn != nil {
		found++
	}

	if match.Like != "" {
		found++
	}

	if found == 0 {
		return fmt.Errorf("no conditions specified")
	}

	return nil
}

func (match Matcher) toSQLConditionClause(ctx context.Context, idx int, conjunction string, colDef orm.ColumnDef, encoderConfig orm.EncodeConfig) (string, map[string]interface{}, error) {
	var (
		queryParts []string
		params     = make(map[string]interface{})
		errs       = new(multierror.Error)
		key        = fmt.Sprintf("%s%d", colDef.Name, idx)
	)

	add := func(operator, suffix string, values ...interface{}) {
		var placeholder []string

		for idx, value := range values {
			encodedValue, err := orm.EncodeValue(ctx, &colDef, value, encoderConfig)
			if err != nil {
				errs.Errors = append(errs.Errors,
					fmt.Errorf("failed to encode %v for column %s: %w", value, colDef.Name, err),
				)
				return
			}

			uniqKey := fmt.Sprintf(":%s%s%d", key, suffix, idx)
			placeholder = append(placeholder, uniqKey)
			params[uniqKey] = encodedValue
		}

		if len(placeholder) == 1 {
			queryParts = append(queryParts, fmt.Sprintf("%s %s %s", colDef.Name, operator, placeholder[0]))
		} else {
			queryParts = append(queryParts, fmt.Sprintf("%s %s ( %s )", colDef.Name, operator, strings.Join(placeholder, ", ")))
		}
	}

	if match.Equal != nil {
		add("=", "eq", match.Equal)
	}

	if match.NotEqual != nil {
		add("!=", "ne", match.NotEqual)
	}

	if match.In != nil {
		add("IN", "in", match.In...)
	}

	if match.NotIn != nil {
		add("NOT IN", "notin", match.NotIn...)
	}

	if match.Like != "" {
		add("LIKE", "like", match.Like)
	}

	if len(queryParts) == 0 {
		// this is an empty matcher without a single condition.
		// we convert that to a no-op TRUE value
		return "( 1 = 1 )", nil, errs.ErrorOrNil()
	}

	if len(queryParts) == 1 {
		return queryParts[0], params, errs.ErrorOrNil()
	}

	return "( " + strings.Join(queryParts, " "+conjunction+" ") + " )", params, errs.ErrorOrNil()
}

func (query Query) toSQLWhereClause(ctx context.Context, m *orm.TableSchema, encoderConfig orm.EncodeConfig) (string, map[string]interface{}, error) {
	if len(query) == 0 {
		return "", nil, nil
	}

	// create a lookup map to validate column names
	lm := make(map[string]orm.ColumnDef, len(m.Columns))
	for _, col := range m.Columns {
		lm[col.Name] = col
	}

	paramMap := make(map[string]interface{})
	columnStmts := make([]string, 0, len(query))

	// get all keys and sort them so we get a stable output
	queryKeys := make([]string, 0, len(query))
	for column := range query {
		queryKeys = append(queryKeys, column)
	}
	sort.Strings(queryKeys)

	// actually create the WHERE clause parts for each
	// column in query.
	errs := new(multierror.Error)
	for _, column := range queryKeys {
		values := query[column]
		colDef, ok := lm[column]
		if !ok {
			errs.Errors = append(errs.Errors, fmt.Errorf("column %s is not allowed", column))

			continue
		}

		queryParts := make([]string, len(values))
		for idx, val := range values {
			matcherQuery, params, err := val.toSQLConditionClause(ctx, idx, "AND", colDef, encoderConfig)
			if err != nil {
				errs.Errors = append(errs.Errors,
					fmt.Errorf("invalid matcher at index %d for column %s: %w", idx, colDef.Name, err),
				)

				continue
			}

			// merge parameters up into the superior parameter map
			for key, val := range params {
				if _, ok := paramMap[key]; ok {
					// is is soley a developer mistake when implementing a matcher so no forgiving ...
					panic("sqlite parameter collision")
				}

				paramMap[key] = val
			}

			queryParts[idx] = matcherQuery
		}

		columnStmts = append(columnStmts,
			fmt.Sprintf("( %s )", strings.Join(queryParts, " OR ")),
		)
	}

	whereClause := strings.Join(columnStmts, " AND ")

	return whereClause, paramMap, errs.ErrorOrNil()
}

func (sel *Selects) UnmarshalJSON(blob []byte) error {
	if len(blob) == 0 {
		return io.ErrUnexpectedEOF
	}

	// if we are looking at a slice directly decode into
	// a []Select
	if blob[0] == '[' {
		var result []Select
		if err := json.Unmarshal(blob, &result); err != nil {
			return err
		}

		(*sel) = result

		return nil
	}

	// if it's an object decode into a single select
	if blob[0] == '{' {
		var result Select
		if err := json.Unmarshal(blob, &result); err != nil {
			return err
		}

		*sel = []Select{result}

		return nil
	}

	// otherwise this is just the field name
	var field string
	if err := json.Unmarshal(blob, &field); err != nil {
		return err
	}

	return nil
}

func (sel *Select) UnmarshalJSON(blob []byte) error {
	if len(blob) == 0 {
		return io.ErrUnexpectedEOF
	}

	// if we have an object at hand decode the select
	// directly
	if blob[0] == '{' {
		var res struct {
			Field string `json:"field"`
			Count *Count `json:"$count"`
		}

		if err := json.Unmarshal(blob, &res); err != nil {
			return err
		}

		sel.Count = res.Count
		sel.Field = res.Field

		if sel.Count != nil && sel.Count.As != "" {
			if !charOnlyRegexp.MatchString(sel.Count.As) {
				return fmt.Errorf("invalid characters in $count.as, value must match [a-zA-Z]+")
			}
		}

		return nil
	}

	var x string
	if err := json.Unmarshal(blob, &x); err != nil {
		return err
	}

	sel.Field = x

	return nil
}

func (orderBys *OrderBys) UnmarshalJSON(blob []byte) error {
	if len(blob) == 0 {
		return io.ErrUnexpectedEOF
	}

	if blob[0] == '[' {
		var result []OrderBy
		if err := json.Unmarshal(blob, &result); err != nil {
			return err
		}

		*orderBys = result

		return nil
	}

	if blob[0] == '{' {
		var result OrderBy
		if err := json.Unmarshal(blob, &result); err != nil {
			return err
		}

		*orderBys = []OrderBy{result}

		return nil
	}

	var field string
	if err := json.Unmarshal(blob, &field); err != nil {
		return err
	}

	*orderBys = []OrderBy{
		{
			Field: field,
			Desc:  false,
		},
	}

	return nil
}

func (orderBy *OrderBy) UnmarshalJSON(blob []byte) error {
	if len(blob) == 0 {
		return io.ErrUnexpectedEOF
	}

	if blob[0] == '{' {
		var res struct {
			Field string `json:"field"`
			Desc  bool   `json:"desc"`
		}

		if err := json.Unmarshal(blob, &res); err != nil {
			return err
		}

		orderBy.Desc = res.Desc
		orderBy.Field = res.Field

		return nil
	}

	var field string
	if err := json.Unmarshal(blob, &field); err != nil {
		return err
	}

	orderBy.Field = field
	orderBy.Desc = false

	return nil
}
