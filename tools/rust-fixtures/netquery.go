package main

import (
	"context"
	"encoding/json"
	"fmt"
	_ "unsafe" // Required for go:linkname.

	"github.com/safing/portmaster/service/netquery"
	"github.com/safing/portmaster/service/netquery/orm"
)

// netquerySQL is the unexported SQL generator of the netquery API handler.
//
//go:linkname netquerySQL github.com/safing/portmaster/service/netquery.(*QueryRequestPayload).generateSQL
func netquerySQL(req *netquery.QueryRequestPayload, ctx context.Context, schema *orm.TableSchema) (string, map[string]interface{}, error)

// Request payloads shaped like the ones the UI sends
// (desktop/angular/projects/safing/portmaster-api/src/lib/netquery.service.ts).
var netqueryRequests = []string{
	`{}`,
	`{"select":"profile"}`,
	`{"select":["profile","domain"],"query":{"profile":"local/abc"}}`,
	`{"query":{"verdict":[2,5,6]},"pageSize":25,"page":2}`,
	`{"query":{"verdict":{"$in":[2,5,6]},"domain":{"$like":"%.example.com."}}}`,
	`{"query":{"remote_port":{"$gt":1000,"$le":2000}},"orderBy":"started"}`,
	`{"query":{"allowed":{"$eq":true},"active":false},"orderBy":[{"field":"started","desc":true},"domain"]}`,
	`{"query":{"started":{"$ge":"2024-01-02T03:04:05Z"}},"databases":["main","history"]}`,
	`{"query":{"started":{"$ge":1700000000},"ended":{"$lt":1700000600}}}`,
	`{"query":{"country":{"$notIn":["DE","US"]},"profile":{"$ne":"special/system"}}}`,
	`{"select":[{"$count":{"field":"*","as":"totalCount"}},"profile"],"groupBy":["profile"],"orderBy":[{"field":"totalCount","desc":true}]}`,
	`{"select":[{"$count":{"field":"domain","distinct":true}}],"groupBy":["country"]}`,
	`{"select":[{"$sum":{"condition":{"verdict":{"$in":[2,5,6]}},"as":"allowed"}},{"$sum":{"field":"bytes_sent","as":"sent"}},"profile"],"groupBy":["profile"]}`,
	`{"select":[{"$min":{"condition":{"verdict":3},"as":"blocked"}},{"$min":{"field":"started","as":"first"}}],"groupBy":["domain"]}`,
	`{"select":[{"$distinct":"profile"}]}`,
	`{"select":[{"$field":{"field":"remote_ip","as":"ip"}}],"query":{"remote_ip":{"$like":"10.%"}}}`,
	`{"textSearch":{"fields":["domain","as_owner","path"],"value":"goo"},"query":{"profile":"x"}}`,
	`{"textSearch":{"fields":[],"value":"x"}}`,
	`{"groupBy":["profile","domain"]}`,
	`{"query":{"scope":{"$in":[1,2]},"ip_version":4,"ip_protocol":{"$in":[6,17]}}}`,
	`{"query":{"started":"2024-01-02T03:04:05+02:00","ended":{"$in":["2024-01-02T03:04:05Z"]}}}`,
	`{"query":{"domain":{"$eq":"a","$ne":"b","$like":"%c"}}}`,
	`{"select":["domain",{"$count":{"field":"domain","as":"cnt"}}],"groupBy":["domain"],"orderBy":"cnt"}`,
	`{"query":{"exit_node":null,"domain":[null,"x"]}}`,
	// Errors.
	`{"query":{"domain":{"$eq":[1]}}}`,
	`{"query":{"started":"yesterday"}}`,
	`{"query":{"nope":"x"}}`,
	`{"select":"nope"}`,
	`{"orderBy":"nope"}`,
	`{"groupBy":["nope"]}`,
	`{"query":{"domain":{}}}`,
	`{"select":[{"$sum":{"field":"bytes_sent"}}]}`,
	`{"select":[{"$count":{"field":"domain","as":"a b"}}]}`,
	`{"textSearch":{"fields":["remote_port"],"value":"1"}}`,
	`{"query":{"domain":[["x"]]}}`,
	`{"query":{"started":{"$ge":"not-a-time"}}}`,
}

func genNetquery() {
	schema, err := orm.GenerateTableSchema("connections", netquery.Conn{})
	if err != nil {
		fail(err)
	}

	var cases []any
	for _, raw := range netqueryRequests {
		c := map[string]any{"request": raw}
		var req netquery.QueryRequestPayload
		if err := json.Unmarshal([]byte(raw), &req); err != nil {
			c["unmarshal_error"] = err.Error()
			cases = append(cases, c)
			continue
		}
		sql, params, err := netquerySQL(&req, context.Background(), schema)
		if err != nil {
			c["error"] = err.Error()
			cases = append(cases, c)
			continue
		}
		c["sql"] = sql
		typed := map[string]any{}
		for k, v := range params {
			typed[k] = map[string]any{"type": fmt.Sprintf("%T", v), "value": v}
		}
		c["params"] = typed
		cases = append(cases, c)
	}

	writeObject("netquery.json", map[string]any{
		"create_main":    schema.CreateStatement("main", true),
		"create_history": schema.CreateStatement("history", false),
		"cases":          cases,
	})
}
