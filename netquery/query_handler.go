package netquery

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/netquery/orm"
)

var (
	charOnlyRegexp = regexp.MustCompile("[a-zA-Z]+")
)

type (

	// QueryHandler implements http.Handler and allows to perform SQL
	// query and aggregate functions on Database.
	QueryHandler struct {
		IsDevMode func() bool
		Database  *Database
	}
)

func (qh *QueryHandler) ServeHTTP(resp http.ResponseWriter, req *http.Request) {
	start := time.Now()
	requestPayload, err := qh.parseRequest(req)
	if err != nil {
		http.Error(resp, err.Error(), http.StatusBadRequest)

		return
	}

	queryParsed := time.Since(start)

	query, paramMap, err := requestPayload.generateSQL(req.Context(), qh.Database.Schema)
	if err != nil {
		http.Error(resp, err.Error(), http.StatusBadRequest)

		return
	}

	sqlQueryBuilt := time.Since(start)

	// actually execute the query against the database and collect the result
	var result []map[string]interface{}
	if err := qh.Database.Execute(
		req.Context(),
		query,
		orm.WithNamedArgs(paramMap),
		orm.WithResult(&result),
	); err != nil {
		http.Error(resp, "Failed to execute query: "+err.Error(), http.StatusInternalServerError)

		return
	}
	sqlQueryFinished := time.Since(start)

	// send the HTTP status code
	resp.WriteHeader(http.StatusOK)

	// prepare the result encoder.
	enc := json.NewEncoder(resp)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")

	// prepare the result body that, in dev mode, contains
	// some diagnostics data about the query
	var resultBody map[string]interface{}
	if qh.IsDevMode() {
		resultBody = map[string]interface{}{
			"sql_prep_stmt": query,
			"sql_params":    paramMap,
			"query":         requestPayload.Query,
			"orderBy":       requestPayload.OrderBy,
			"groupBy":       requestPayload.GroupBy,
			"selects":       requestPayload.Select,
			"times": map[string]interface{}{
				"start_time":           start,
				"query_parsed_after":   queryParsed.String(),
				"query_built_after":    sqlQueryBuilt.String(),
				"query_executed_after": sqlQueryFinished.String(),
			},
		}
	} else {
		resultBody = make(map[string]interface{})
	}
	resultBody["results"] = result

	// and finally stream the response
	if err := enc.Encode(resultBody); err != nil {
		// we failed to encode the JSON body to resp so we likely either already sent a
		// few bytes or the pipe was already closed. In either case, trying to send the
		// error using http.Error() is non-sense. We just log it out here and that's all
		// we can do.
		log.Errorf("failed to encode JSON response: %s", err)

		return
	}
}

func (qh *QueryHandler) parseRequest(req *http.Request) (*QueryRequestPayload, error) {
	var body io.Reader

	switch req.Method {
	case http.MethodPost, http.MethodPut:
		body = req.Body
	case http.MethodGet:
		body = strings.NewReader(req.URL.Query().Get("q"))
	default:
		return nil, fmt.Errorf("invalid HTTP method")
	}

	var requestPayload QueryRequestPayload
	blob, err := ioutil.ReadAll(body)
	if err != nil {
		return nil, fmt.Errorf("failed to read body" + err.Error())
	}

	body = bytes.NewReader(blob)

	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()

	if err := json.Unmarshal(blob, &requestPayload); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("invalid query: %w", err)
	}

	return &requestPayload, nil
}

func (req *QueryRequestPayload) generateSQL(ctx context.Context, schema *orm.TableSchema) (string, map[string]interface{}, error) {
	if err := req.prepareSelectedFields(schema); err != nil {
		return "", nil, fmt.Errorf("perparing selected fields: %w", err)
	}

	// build the SQL where clause from the payload query
	whereClause, paramMap, err := req.Query.toSQLWhereClause(
		ctx,
		schema,
		orm.DefaultEncodeConfig,
	)
	if err != nil {
		return "", nil, fmt.Errorf("ganerating where clause: %w", err)
	}

	// build the actual SQL query statement
	// FIXME(ppacher): add support for group-by and sort-by

	groupByClause, err := req.generateGroupByClause(schema)
	if err != nil {
		return "", nil, fmt.Errorf("generating group-by clause: %w", err)
	}

	orderByClause, err := req.generateOrderByClause(schema)
	if err != nil {
		return "", nil, fmt.Errorf("generating order-by clause: %w", err)
	}

	selectClause := req.generateSelectClause()
	query := `SELECT ` + selectClause + ` FROM connections`
	if whereClause != "" {
		query += " WHERE " + whereClause
	}
	query += " " + groupByClause + " " + orderByClause

	return query, paramMap, nil
}

func (req *QueryRequestPayload) prepareSelectedFields(schema *orm.TableSchema) error {
	for _, s := range req.Select {
		var field string
		if s.Count != nil {
			field = s.Count.Field
		} else {
			field = s.Field
		}

		colName := "*"
		if field != "*" || s.Count == nil {
			var err error

			colName, err = req.validateColumnName(schema, field)
			if err != nil {
				return err
			}
		}

		if s.Count != nil {
			var as = s.Count.As
			if as == "" {
				as = fmt.Sprintf("%s_count", colName)
			}
			var distinct = ""
			if s.Count.Distinct {
				distinct = "DISTINCT "
			}
			req.selectedFields = append(req.selectedFields, fmt.Sprintf("COUNT(%s%s) as %s", distinct, colName, as))
			req.whitelistedFields = append(req.whitelistedFields, as)
		} else {
			req.selectedFields = append(req.selectedFields, colName)
		}
	}

	return nil
}

func (req *QueryRequestPayload) generateGroupByClause(schema *orm.TableSchema) (string, error) {
	if len(req.GroupBy) == 0 {
		return "", nil
	}

	var groupBys = make([]string, len(req.GroupBy))

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
	var selectClause = "*"
	if len(req.selectedFields) > 0 {
		selectClause = strings.Join(req.selectedFields, ", ")
	}

	return selectClause
}

func (req *QueryRequestPayload) generateOrderByClause(schema *orm.TableSchema) (string, error) {
	var orderBys = make([]string, len(req.OrderBy))
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

	for _, selected := range req.whitelistedFields {
		if field == selected {
			return field, nil
		}
	}

	for _, selected := range req.selectedFields {
		if field == selected {
			return field, nil
		}
	}

	return "", fmt.Errorf("column name %s not allowed", field)
}

// compile time check
var _ http.Handler = new(QueryHandler)
