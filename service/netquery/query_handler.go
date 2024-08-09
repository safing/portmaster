package netquery

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/hashicorp/go-multierror"
	servertiming "github.com/mitchellh/go-server-timing"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/netquery/orm"
)

var charOnlyRegexp = regexp.MustCompile("[a-zA-Z]+")

const failedQuery = "Failed to execute query: "

type (

	// QueryHandler implements http.Handler and allows to perform SQL
	// query and aggregate functions on Database.
	QueryHandler struct {
		IsDevMode func() bool
		Database  *Database
	}

	// BatchQueryHandler implements http.Handler and allows to perform SQL
	// query and aggregate functions on Database in batches.
	BatchQueryHandler struct {
		IsDevMode func() bool
		Database  *Database
	}
)

func (qh *QueryHandler) ServeHTTP(resp http.ResponseWriter, req *http.Request) {
	timing := servertiming.FromContext(req.Context())

	timingQueryParsed := timing.NewMetric("query_parsed").
		WithDesc("Query has been parsed").
		Start()

	requestPayload, err := parseQueryRequestPayload[QueryRequestPayload](req)
	if err != nil {
		http.Error(resp, err.Error(), http.StatusBadRequest)

		return
	}

	timingQueryParsed.Stop()

	timingQueryBuilt := timing.NewMetric("query_built").
		WithDesc("The SQL query has been built").
		Start()

	query, paramMap, err := requestPayload.generateSQL(req.Context(), qh.Database.Schema)
	if err != nil {
		http.Error(resp, err.Error(), http.StatusBadRequest)

		return
	}

	timingQueryBuilt.Stop()

	timingQueryExecute := timing.NewMetric("sql_exec").
		WithDesc("SQL query execution time").
		Start()

	// actually execute the query against the database and collect the result
	var result []map[string]interface{}
	if err := qh.Database.Execute(
		req.Context(),
		query,
		orm.WithNamedArgs(paramMap),
		orm.WithResult(&result),
		orm.WithSchema(*qh.Database.Schema),
	); err != nil {
		http.Error(resp, failedQuery+err.Error(), http.StatusInternalServerError)

		return
	}
	timingQueryExecute.Stop()

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

func (batch *BatchQueryHandler) ServeHTTP(resp http.ResponseWriter, req *http.Request) {
	timing := servertiming.FromContext(req.Context())

	timingQueryParsed := timing.NewMetric("query_parsed").
		WithDesc("Query has been parsed").
		Start()

	requestPayload, err := parseQueryRequestPayload[BatchQueryRequestPayload](req)
	if err != nil {
		http.Error(resp, err.Error(), http.StatusBadRequest)

		return
	}

	timingQueryParsed.Stop()

	response := make(map[string][]map[string]any, len(*requestPayload))

	batches := make([]BatchExecute, 0, len(*requestPayload))

	for key, query := range *requestPayload {

		timingQueryBuilt := timing.NewMetric("query_built_" + key).
			WithDesc("The SQL query has been built").
			Start()

		sql, paramMap, err := query.generateSQL(req.Context(), batch.Database.Schema)
		if err != nil {
			http.Error(resp, err.Error(), http.StatusBadRequest)

			return
		}

		timingQueryBuilt.Stop()

		var result []map[string]any
		batches = append(batches, BatchExecute{
			ID:     key,
			SQL:    sql,
			Params: paramMap,
			Result: &result,
		})
	}

	timingQueryExecute := timing.NewMetric("sql_exec").
		WithDesc("SQL query execution time").
		Start()

	status := http.StatusOK
	if err := batch.Database.ExecuteBatch(req.Context(), batches); err != nil {
		status = http.StatusInternalServerError

		var merr *multierror.Error
		if errors.As(err, &merr) {
			for _, e := range merr.Errors {
				resp.Header().Add("X-Query-Error", e.Error())
			}
		} else {
			// Should not happen, ExecuteBatch always returns a multierror.Error
			resp.WriteHeader(status)

			return
		}
	}

	timingQueryExecute.Stop()

	// collect the results
	for _, b := range batches {
		response[b.ID] = *b.Result
	}

	// send the HTTP status code
	resp.WriteHeader(status)

	// prepare the result encoder.
	enc := json.NewEncoder(resp)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")

	// and finally stream the response
	if err := enc.Encode(response); err != nil {
		// we failed to encode the JSON body to resp so we likely either already sent a
		// few bytes or the pipe was already closed. In either case, trying to send the
		// error using http.Error() is non-sense. We just log it out here and that's all
		// we can do.
		log.Errorf("failed to encode JSON response: %s", err)

		return
	}
}

func parseQueryRequestPayload[T any](req *http.Request) (*T, error) { //nolint:dupl
	var (
		body           io.Reader
		requestPayload T
	)

	switch req.Method {
	case http.MethodPost, http.MethodPut:
		body = req.Body
	case http.MethodGet:
		body = strings.NewReader(req.URL.Query().Get("q"))
	default:
		return nil, fmt.Errorf("invalid HTTP method")
	}

	blob, err := io.ReadAll(body)
	if err != nil {
		return nil, fmt.Errorf("failed to read body: %w", err)
	}

	body = bytes.NewReader(blob)

	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()

	if err := json.Unmarshal(blob, &requestPayload); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("invalid query: %w", err)
	}

	return &requestPayload, nil
}

// Compile time check.
var _ http.Handler = new(QueryHandler)
