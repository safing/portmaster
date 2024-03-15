package netquery

import (
	"context"
	"fmt"
	"strings"

	"golang.org/x/exp/slices"

	"github.com/safing/portmaster/service/netquery/orm"
)

type (
	// QueryRequestPayload describes the payload of a netquery query.
	QueryRequestPayload struct {
		Select     Selects     `json:"select"`
		Query      Query       `json:"query"`
		OrderBy    OrderBys    `json:"orderBy"`
		GroupBy    []string    `json:"groupBy"`
		TextSearch *TextSearch `json:"textSearch"`
		// A list of databases to query. If left empty,
		// both, the LiveDatabase and the HistoryDatabase are queried
		Databases []DatabaseName `json:"databases"`

		Pagination

		selectedFields    []string
		whitelistedFields []string
		paramMap          map[string]interface{}
	}

	// BatchQueryRequestPayload describes the payload of a batch netquery
	// query. The map key is used in the response to identify the results
	// for each query of the batch request.
	BatchQueryRequestPayload map[string]QueryRequestPayload
)

func (req *QueryRequestPayload) generateSQL(ctx context.Context, schema *orm.TableSchema) (string, map[string]interface{}, error) {
	if err := req.prepareSelectedFields(ctx, schema); err != nil {
		return "", nil, fmt.Errorf("perparing selected fields: %w", err)
	}

	// build the SQL where clause from the payload query
	whereClause, paramMap, err := req.Query.toSQLWhereClause(
		ctx,
		"",
		schema,
		orm.DefaultEncodeConfig,
	)
	if err != nil {
		return "", nil, fmt.Errorf("generating where clause: %w", err)
	}

	req.mergeParams(paramMap)

	if req.TextSearch != nil {
		textClause, textParams, err := req.TextSearch.toSQLConditionClause(ctx, schema, "", orm.DefaultEncodeConfig)
		if err != nil {
			return "", nil, fmt.Errorf("generating text-search clause: %w", err)
		}

		if textClause != "" {
			if whereClause != "" {
				whereClause += " AND "
			}

			whereClause += textClause

			req.mergeParams(textParams)
		}
	}

	groupByClause, err := req.generateGroupByClause(schema)
	if err != nil {
		return "", nil, fmt.Errorf("generating group-by clause: %w", err)
	}

	orderByClause, err := req.generateOrderByClause(schema)
	if err != nil {
		return "", nil, fmt.Errorf("generating order-by clause: %w", err)
	}

	selectClause := req.generateSelectClause()

	if whereClause != "" {
		whereClause = "WHERE " + whereClause
	}

	// if no database is specified we default to LiveDatabase only.
	if len(req.Databases) == 0 {
		req.Databases = []DatabaseName{LiveDatabase}
	}

	sources := make([]string, len(req.Databases))
	for idx, db := range req.Databases {
		sources[idx] = fmt.Sprintf("SELECT * FROM %s.connections %s", db, whereClause)
	}

	source := strings.Join(sources, " UNION ")

	query := `SELECT ` + selectClause + ` FROM ( ` + source + ` ) `

	query += " " + groupByClause + " " + orderByClause + " " + req.Pagination.toSQLLimitOffsetClause()

	return strings.TrimSpace(query), req.paramMap, nil
}

func (req *QueryRequestPayload) prepareSelectedFields(ctx context.Context, schema *orm.TableSchema) error {
	for idx, s := range req.Select {
		var field string

		switch {
		case s.Count != nil:
			field = s.Count.Field
		case s.Distinct != nil:
			field = *s.Distinct
		case s.Sum != nil:
			if s.Sum.Field != "" {
				field = s.Sum.Field
			} else {
				field = "*"
			}
		case s.Min != nil:
			if s.Min.Field != "" {
				field = s.Min.Field
			} else {
				field = "*"
			}
		case s.FieldSelect != nil:
			field = s.FieldSelect.Field
		default:
			field = s.Field
		}

		colName := "*"
		if field != "*" || (s.Count == nil && s.Sum == nil) {
			var err error

			colName, err = req.validateColumnName(schema, field)
			if err != nil {
				return err
			}
		}

		switch {
		case s.FieldSelect != nil:
			as := s.FieldSelect.As
			if as == "" {
				as = s.FieldSelect.Field
			}

			req.selectedFields = append(
				req.selectedFields,
				fmt.Sprintf("%s AS %s", s.FieldSelect.Field, as),
			)
			req.whitelistedFields = append(req.whitelistedFields, as)

		case s.Count != nil:
			as := s.Count.As
			if as == "" {
				as = fmt.Sprintf("%s_count", colName)
			}
			distinct := ""
			if s.Count.Distinct {
				distinct = "DISTINCT "
			}
			req.selectedFields = append(
				req.selectedFields,
				fmt.Sprintf("COUNT(%s%s) AS %s", distinct, colName, as),
			)
			req.whitelistedFields = append(req.whitelistedFields, as)

		case s.Sum != nil:
			if s.Sum.As == "" {
				return fmt.Errorf("missing 'as' for $sum")
			}

			var (
				clause string
				params map[string]any
			)

			if s.Sum.Field != "" {
				clause = s.Sum.Field
			} else {
				var err error
				clause, params, err = s.Sum.Condition.toSQLWhereClause(ctx, fmt.Sprintf("sel%d", idx), schema, orm.DefaultEncodeConfig)
				if err != nil {
					return fmt.Errorf("in $sum: %w", err)
				}
			}

			req.mergeParams(params)
			req.selectedFields = append(
				req.selectedFields,
				fmt.Sprintf("SUM(%s) AS %s", clause, s.Sum.As),
			)
			req.whitelistedFields = append(req.whitelistedFields, s.Sum.As)

		case s.Min != nil:
			if s.Min.As == "" {
				return fmt.Errorf("missing 'as' for $min")
			}

			var (
				clause string
				params map[string]any
			)

			if s.Min.Field != "" {
				clause = field
			} else {
				var err error
				clause, params, err = s.Min.Condition.toSQLWhereClause(ctx, fmt.Sprintf("sel%d", idx), schema, orm.DefaultEncodeConfig)
				if err != nil {
					return fmt.Errorf("in $min: %w", err)
				}
			}

			req.mergeParams(params)
			req.selectedFields = append(
				req.selectedFields,
				fmt.Sprintf("MIN(%s) AS %s", clause, s.Min.As),
			)
			req.whitelistedFields = append(req.whitelistedFields, s.Min.As)

		case s.Distinct != nil:
			req.selectedFields = append(req.selectedFields, fmt.Sprintf("DISTINCT %s", colName))
			req.whitelistedFields = append(req.whitelistedFields, colName)

		default:
			req.selectedFields = append(req.selectedFields, colName)
		}
	}

	return nil
}

func (req *QueryRequestPayload) mergeParams(params map[string]any) {
	if req.paramMap == nil {
		req.paramMap = make(map[string]any)
	}

	for key, value := range params {
		req.paramMap[key] = value
	}
}

func (req *QueryRequestPayload) generateGroupByClause(schema *orm.TableSchema) (string, error) {
	if len(req.GroupBy) == 0 {
		return "", nil
	}

	groupBys := make([]string, len(req.GroupBy))
	for idx, name := range req.GroupBy {
		colName, err := req.validateColumnName(schema, name)
		if err != nil {
			return "", err
		}

		groupBys[idx] = colName
	}
	groupByClause := "GROUP BY " + strings.Join(groupBys, ", ")

	// if there are no explicitly selected fields we default to the
	// group-by columns as that's what's expected most of the time anyway...
	if len(req.selectedFields) == 0 {
		req.selectedFields = append(req.selectedFields, groupBys...)
	}

	return groupByClause, nil
}

func (req *QueryRequestPayload) generateSelectClause() string {
	selectClause := "*"
	if len(req.selectedFields) > 0 {
		selectClause = strings.Join(req.selectedFields, ", ")
	}

	return selectClause
}

func (req *QueryRequestPayload) generateOrderByClause(schema *orm.TableSchema) (string, error) {
	if len(req.OrderBy) == 0 {
		return "", nil
	}

	orderBys := make([]string, len(req.OrderBy))
	for idx, sort := range req.OrderBy {
		colName, err := req.validateColumnName(schema, sort.Field)
		if err != nil {
			return "", err
		}

		if sort.Desc {
			orderBys[idx] = fmt.Sprintf("%s DESC", colName)
		} else {
			orderBys[idx] = fmt.Sprintf("%s ASC", colName)
		}
	}

	return "ORDER BY " + strings.Join(orderBys, ", "), nil
}

func (req *QueryRequestPayload) validateColumnName(schema *orm.TableSchema, field string) (string, error) {
	colDef := schema.GetColumnDef(field)
	if colDef != nil {
		return colDef.Name, nil
	}

	if slices.Contains(req.whitelistedFields, field) {
		return field, nil
	}

	if slices.Contains(req.selectedFields, field) {
		return field, nil
	}

	return "", fmt.Errorf("column name %q not allowed", field)
}
