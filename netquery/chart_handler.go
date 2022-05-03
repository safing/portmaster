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
	"strings"

	"github.com/safing/portmaster/netquery/orm"
)

type ChartHandler struct {
	Database *Database
}

func (ch *ChartHandler) ServeHTTP(resp http.ResponseWriter, req *http.Request) {
	requestPayload, err := ch.parseRequest(req)
	if err != nil {
		http.Error(resp, err.Error(), http.StatusBadRequest)

		return
	}

	query, paramMap, err := requestPayload.generateSQL(req.Context(), ch.Database.Schema)
	if err != nil {
		http.Error(resp, err.Error(), http.StatusBadRequest)

		return
	}

	// actually execute the query against the database and collect the result
	var result []map[string]interface{}
	if err := ch.Database.Execute(
		req.Context(),
		query,
		orm.WithNamedArgs(paramMap),
		orm.WithResult(&result),
		orm.WithSchema(*ch.Database.Schema),
	); err != nil {
		http.Error(resp, "Failed to execute query: "+err.Error(), http.StatusInternalServerError)

		return
	}

	// send the HTTP status code
	resp.WriteHeader(http.StatusOK)

	// prepare the result encoder.
	enc := json.NewEncoder(resp)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")

	enc.Encode(map[string]interface{}{
		"results": result,
		"query":   query,
		"params":  paramMap,
	})
}

func (ch *ChartHandler) parseRequest(req *http.Request) (*QueryActiveConnectionChartPayload, error) {
	var body io.Reader

	switch req.Method {
	case http.MethodPost, http.MethodPut:
		body = req.Body
	case http.MethodGet:
		body = strings.NewReader(req.URL.Query().Get("q"))
	default:
		return nil, fmt.Errorf("invalid HTTP method")
	}

	var requestPayload QueryActiveConnectionChartPayload
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

func (req *QueryActiveConnectionChartPayload) generateSQL(ctx context.Context, schema *orm.TableSchema) (string, map[string]interface{}, error) {
	template := `
WITH RECURSIVE epoch(x) AS (
	SELECT strftime('%%s')-600
	UNION ALL
		SELECT x+1 FROM epoch WHERE x+1 < strftime('%%s')+0
)
SELECT x as timestamp, COUNT(*) AS value FROM epoch
	JOIN connections
	ON strftime('%%s', connections.started)+0 <= timestamp+0 AND (connections.ended IS NULL OR strftime('%%s', connections.ended)+0 > timestamp+0)
		%s
	GROUP BY round(timestamp/10, 0)*10;`

	clause, params, err := req.Query.toSQLWhereClause(ctx, "", schema, orm.DefaultEncodeConfig)
	if err != nil {
		return "", nil, err
	}

	if clause == "" {
		return fmt.Sprintf(template, ""), map[string]interface{}{}, nil
	}

	return fmt.Sprintf(template, "WHERE ( "+clause+")"), params, nil
}
