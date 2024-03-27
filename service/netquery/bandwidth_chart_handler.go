package netquery

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/safing/portmaster/service/netquery/orm"
)

// BandwidthChartHandler handles requests for connection charts.
type BandwidthChartHandler struct {
	Database *Database
}

// BandwidthChartRequest holds a request for a bandwidth chart.
type BandwidthChartRequest struct {
	Interval int      `json:"interval"`
	Query    Query    `json:"query"`
	GroupBy  []string `json:"groupBy"`
}

func (ch *BandwidthChartHandler) ServeHTTP(resp http.ResponseWriter, req *http.Request) { //nolint:dupl
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
		http.Error(resp, failedQuery+err.Error(), http.StatusInternalServerError)

		return
	}

	// send the HTTP status code
	resp.WriteHeader(http.StatusOK)

	// prepare the result encoder.
	enc := json.NewEncoder(resp)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")

	_ = enc.Encode(map[string]interface{}{ //nolint:errchkjson
		"results": result,
		"query":   query,
		"params":  paramMap,
	})
}

func (ch *BandwidthChartHandler) parseRequest(req *http.Request) (*BandwidthChartRequest, error) { //nolint:dupl
	var body io.Reader

	switch req.Method {
	case http.MethodPost, http.MethodPut:
		body = req.Body
	case http.MethodGet:
		body = strings.NewReader(req.URL.Query().Get("q"))
	default:
		return nil, fmt.Errorf("invalid HTTP method")
	}

	var requestPayload BandwidthChartRequest
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

func (req *BandwidthChartRequest) generateSQL(ctx context.Context, schema *orm.TableSchema) (string, map[string]interface{}, error) {
	if req.Interval == 0 {
		req.Interval = 10
	}

	interval := fmt.Sprintf("round(time/%d, 0)*%d", req.Interval, req.Interval)

	// make sure there are only allowed fields specified in the request group-by
	for _, gb := range req.GroupBy {
		def := schema.GetColumnDef(gb)
		if def == nil {
			return "", nil, fmt.Errorf("unsupported groupBy key: %q", gb)
		}
	}

	selects := append([]string{
		interval + " as timestamp",
		"SUM(incoming) as incoming",
		"SUM(outgoing) as outgoing",
	}, req.GroupBy...)

	groupBy := append([]string{interval}, req.GroupBy...)

	whereClause, params, err := req.Query.toSQLWhereClause(ctx, "", schema, orm.DefaultEncodeConfig)
	if err != nil {
		return "", nil, err
	}

	if whereClause != "" {
		whereClause = "WHERE " + whereClause
	}

	template := fmt.Sprintf(
		`SELECT %s
		FROM main.bandwidth AS bw
		JOIN main.connections AS conns
			ON bw.conn_id = conns.id
		%s
		GROUP BY %s
		ORDER BY time ASC`,
		strings.Join(selects, ", "),
		whereClause,
		strings.Join(groupBy, ", "),
	)

	return template, params, nil
}
