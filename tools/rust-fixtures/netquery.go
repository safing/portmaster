package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"
	"unsafe" // Also required for go:linkname.

	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"

	"github.com/safing/portmaster/service/intel"
	"github.com/safing/portmaster/service/netquery"
	"github.com/safing/portmaster/service/network"
	"github.com/safing/portmaster/service/process"
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

// netqueryConvert is the unexported network.Connection to Conn conversion.
//
//go:linkname netqueryConvert github.com/safing/portmaster/service/netquery.convertConnection
func netqueryConvert(conn *network.Connection) (*netquery.Conn, error)

func setUnexported(ptr any, field string, value any) {
	f := reflect.ValueOf(ptr).Elem().FieldByName(field)
	reflect.NewAt(f.Type(), unsafe.Pointer(f.UnsafeAddr())).Elem().Set(reflect.ValueOf(value))
}

// genNetqueryConvert runs the connections of the network fixture through
// convertConnection and marshals the result like the runtime push does.
func genNetqueryConvert() []any {
	cases := append([]namedConn{}, networkTestConns...)
	cases = append(cases, namedConn{"no_entity_inbound", 7, &network.Connection{
		ID:      "no-entity",
		Inbound: true,
		Started: 1700000600,
		ProcessContext: network.ProcessContext{
			PID: 7, Source: "special", Profile: "system", ProfileName: "System",
		},
	}}, namedConn{"empty_entity_ended", 8, &network.Connection{
		ID:      "empty-entity",
		Type:    network.IPConnection,
		Entity:  &intel.Entity{},
		Started: 1700000700,
		Ended:   1700000800,
	}})

	var out []any
	for _, c := range cases {
		key := fmt.Sprintf("%d-1700000000", c.pid)
		proc := &process.Process{}
		setUnexported(proc, "processKey", key)
		setUnexported(c.conn, "process", proc)

		model, err := netqueryConvert(c.conn)
		if err != nil {
			fail(err)
		}
		blob, err := json.Marshal(model)
		if err != nil {
			fail(err)
		}
		out = append(out, map[string]any{
			"name":        c.name,
			"process_key": key,
			"json":        string(blob),
			"extra_data":  string(model.ExtraData),
		})
	}
	return out
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
		"rows":           genNetqueryResults(),
		"converted":      genNetqueryConvert(),
	})
}

// netqueryConns are stored with the same upsert the Go Database.Save uses.
func netqueryConns() []netquery.Conn {
	started := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	ended := started.Add(90 * time.Second)
	yes, no := true, false
	exit := "exit-node-1"
	return []netquery.Conn{
		{
			ID: "proc-1-conn-a", ProfileID: "local/abc", Path: "/usr/bin/curl", Type: "ip",
			External: true, IPVersion: 4, IPProtocol: 6, LocalIP: "10.0.0.2", LocalPort: 40000,
			RemoteIP: "93.184.216.34", RemotePort: 443, Domain: "example.com.", Country: "US",
			ASN: 15133, ASOwner: "Edgecast <Inc>", Latitude: 37.751, Longitude: -97.822,
			Scope: 4, WorstVerdict: 2, ActiveVerdict: 2, FirewallVerdict: 2, Started: started,
			Encrypted: true, Direction: "outbound", ExtraData: []byte(`{"pid":12,"reason":{"Msg":"allowed"}}`),
			Allowed: &yes, ProfileRevision: 3, Active: true, ProfileName: "curl",
		},
		{
			ID: "proc-2-conn-b", ProfileID: "special/system", Type: "dns", Domain: "blocked.test.",
			IPVersion: 6, IPProtocol: 17, LocalIP: "::1", RemoteIP: "2001:db8::1", RemotePort: 53,
			WorstVerdict: 3, ActiveVerdict: 3, FirewallVerdict: 3, Started: started.Add(time.Second),
			Ended: &ended, Tunneled: true, Internal: true, Direction: "inbound", ExtraData: []byte(`{}`),
			Allowed: &no, ExitNode: &exit, BytesReceived: 99, BytesSent: 77, ProfileName: "System",
		},
	}
}

var netqueryResultQueries = []string{
	`{}`,
	`{"select":["profile",{"$count":{"field":"*","as":"totalCount"}},{"$sum":{"field":"bytes_sent","as":"sent"}}],"groupBy":["profile"],"orderBy":["profile"]}`,
	`{"select":["allowed",{"$min":{"field":"started","as":"first"}}],"groupBy":["allowed"],"orderBy":["allowed"]}`,
	`{"select":[{"$distinct":"profile"}],"orderBy":["profile"]}`,
	`{"query":{"active":false},"select":["id","ended","exit_node","allowed","extra_data"]}`,
	`{"textSearch":{"fields":["as_owner"],"value":"<inc"},"select":["as_owner","latitude"]}`,
}

func genNetqueryResults() map[string]any {
	ctx := context.Background()
	schema, err := orm.GenerateTableSchema("connections", netquery.Conn{})
	if err != nil {
		fail(err)
	}
	conn, err := sqlite.OpenConn("file:nqfixture?mode=memory", sqlite.OpenCreate, sqlite.OpenReadWrite, sqlite.OpenURI)
	if err != nil {
		fail(err)
	}
	defer conn.Close()
	if err := sqlitex.ExecuteTransient(conn, schema.CreateStatement("main", true), nil); err != nil {
		fail(err)
	}

	var encoded []any
	for _, c := range netqueryConns() {
		m, err := orm.ToParamMap(ctx, c, "", orm.DefaultEncodeConfig, []string{"bytes_received", "bytes_sent"})
		if err != nil {
			fail(err)
		}
		typed := map[string]any{}
		keys := make([]string, 0, len(m))
		for k, v := range m {
			typed[k] = map[string]any{"type": fmt.Sprintf("%T", v), "value": v}
			keys = append(keys, k)
		}
		encoded = append(encoded, typed)
		sort.Strings(keys)
		named := map[string]any{}
		var cols, ph []string
		for _, k := range keys {
			cols = append(cols, k)
			ph = append(ph, ":"+k)
			named[":"+k] = m[k]
		}
		sql := "INSERT INTO main.connections (" + strings.Join(cols, ", ") + ") VALUES(" + strings.Join(ph, ", ") + ")"
		if err := sqlitex.Execute(conn, sql, &sqlitex.ExecOptions{Named: named}); err != nil {
			fail(err)
		}
		// Byte counters are only written by UpdateBandwidth.
		if c.BytesSent > 0 {
			if err := sqlitex.Execute(conn, "UPDATE main.connections SET bytes_received = :r, bytes_sent = :s WHERE id = :id",
				&sqlitex.ExecOptions{Named: map[string]any{":r": c.BytesReceived, ":s": c.BytesSent, ":id": c.ID}}); err != nil {
				fail(err)
			}
		}
	}

	var results []any
	for _, raw := range netqueryResultQueries {
		var req netquery.QueryRequestPayload
		if err := json.Unmarshal([]byte(raw), &req); err != nil {
			fail(err)
		}
		sql, params, err := netquerySQL(&req, ctx, schema)
		if err != nil {
			fail(fmt.Errorf("%s: %w", raw, err))
		}
		out := map[string]any{"request": raw}
		for _, mode := range []string{"schema", "batch"} {
			var rows []map[string]any
			opts := []orm.QueryOption{orm.WithNamedArgs(params), orm.WithResult(&rows)}
			if mode == "schema" {
				opts = append(opts, orm.WithSchema(*schema))
			}
			if err := orm.RunQuery(ctx, conn, sql, opts...); err != nil {
				fail(fmt.Errorf("%s: %w", raw, err))
			}
			var buf bytes.Buffer
			enc := json.NewEncoder(&buf)
			enc.SetEscapeHTML(false)
			enc.SetIndent("", "  ")
			if err := enc.Encode(rows); err != nil {
				fail(err)
			}
			out[mode] = buf.String()
		}
		results = append(results, out)
	}
	return map[string]any{"encoded": encoded, "results": results}
}
