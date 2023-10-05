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

	"github.com/safing/portmaster/netquery/orm"
)

// BandwidthChartHandler handles requests for connection charts.
type BandwidthChartHandler struct {
	Database *Database
}

type BandwidthChartRequest struct {
	AllProfiles bool     `json:"allProfiles"`
	Profiles    []string `json:"profiles"`
	Connections []string `json:"connections"`
}

func (ch *BandwidthChartHandler) ServeHTTP(resp http.ResponseWriter, req *http.Request) {
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

func (req *BandwidthChartRequest) generateSQL(ctx context.Context, schema *orm.TableSchema) (string, map[string]interface{}, error) {
	selects := []string{
		"(round(time/10, 0)*10) as time",
		"SUM(incoming) as incoming",
		"SUM(outgoing) as outgoing",
	}
	groupBy := []string{"round(time/10, 0)*10"}
	whereClause := ""
	params := make(map[string]any)

	if (len(req.Profiles) > 0) || (req.AllProfiles == true) {
		groupBy = []string{"profile", "round(time/10, 0)*10"}
		selects = append(selects, "profile")

		if !req.AllProfiles {
			clauses := make([]string, len(req.Profiles))

			for idx, p := range req.Profiles {
				key := fmt.Sprintf(":p%d", idx)
				clauses[idx] = "profile = " + key
				params[key] = p
			}

			whereClause = "WHERE " + strings.Join(clauses, " OR ")
		}
	} else if len(req.Connections) > 0 {
		groupBy = []string{"conn_id", "round(time/10, 0)*10"}
		selects = append(selects, "conn_id")

		clauses := make([]string, len(req.Connections))

		for idx, p := range req.Connections {
			key := fmt.Sprintf(":c%d", idx)
			clauses[idx] = "conn_id = " + key
			params[key] = p
		}

		whereClause = "WHERE " + strings.Join(clauses, " OR ")
	}

	template := fmt.Sprintf(
		`SELECT %s FROM main.bandwidth %s GROUP BY %s ORDER BY time ASC`,
		strings.Join(selects, ", "),
		whereClause,
		strings.Join(groupBy, ", "),
	)

	return template, params, nil
}
