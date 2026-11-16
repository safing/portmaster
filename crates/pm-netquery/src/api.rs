//! The `netquery/*` API endpoints (Go `module_api.go`, `query_handler.go`,
//! `active_chart_handler.go`, `bandwidth_chart_handler.go`).
//!
//! The Go `servertiming` middleware is not ported. Handler responses carry no
//! explicit content type: Go lets `net/http` sniff the JSON body, which
//! yields `text/plain; charset=utf-8`.
//!
//! Batch generation errors report the first failing key in sorted order; Go
//! iterates a map, so its choice is random.

use std::collections::BTreeMap;
use std::sync::Arc;

use http::{HeaderName, HeaderValue};
use pm_api::{Api, ApiError, Endpoint, Request, Response, PERMIT_USER};
use serde_json::{json, Map, Value};

use crate::db::{Database, DbError, Row};
use crate::query::{chart_where_clause, generate_sql, Matcher, Param, QueryRequest, Select};
use crate::schema::column;

const FAILED_QUERY: &str = "Failed to execute query: ";
const TEXT_PLAIN: &str = "text/plain; charset=utf-8";

/// Environment of the endpoints that lives outside this crate.
pub struct ApiOptions {
    /// Whether the dev-mode diagnostics are added to query responses.
    pub dev_mode: Arc<dyn Fn() -> bool + Send + Sync>,
    /// Retention in days for a stored profile ID (`local/<id>`); 0 keeps
    /// the history forever.
    pub retention_days: Arc<dyn Fn(&str) -> i64 + Send + Sync>,
    /// Current time in unix seconds.
    pub now_unix: Arc<dyn Fn() -> i64 + Send + Sync>,
}

impl ApiOptions {
    /// Options using the system clock.
    pub fn new(
        dev_mode: Arc<dyn Fn() -> bool + Send + Sync>,
        retention_days: Arc<dyn Fn(&str) -> i64 + Send + Sync>,
    ) -> ApiOptions {
        ApiOptions {
            dev_mode,
            retention_days,
            now_unix: Arc::new(|| {
                std::time::SystemTime::now()
                    .duration_since(std::time::UNIX_EPOCH)
                    .map_or(0, |d| i64::try_from(d.as_secs()).unwrap_or(i64::MAX))
            }),
        }
    }
}

/// Registers every netquery endpoint.
pub fn register_api(api: &Api, db: Arc<Database>, opts: ApiOptions) -> Result<(), ApiError> {
    let opts = Arc::new(opts);

    let (d, o) = (db.clone(), opts.clone());
    api.register_endpoint(
        Endpoint::new(
            "netquery/query",
            "Query Connections",
            "Query the in-memory sqlite connection database.",
        )
        .with_mime_type("application/json")
        .with_read(PERMIT_USER)
        .with_write(PERMIT_USER)
        .with_handler(move |r| query_handler(&d, &o, r)),
    )?;

    let d = db.clone();
    api.register_endpoint(
        Endpoint::new(
            "netquery/query/batch",
            "Batch Query Connections",
            "Batch query the in-memory sqlite connection database.",
        )
        .with_mime_type("application/json")
        .with_read(PERMIT_USER)
        .with_write(PERMIT_USER)
        .with_handler(move |r| batch_handler(&d, r)),
    )?;

    let d = db.clone();
    api.register_endpoint(
        Endpoint::new(
            "netquery/charts/connection-active",
            "Active Connections Chart",
            "Query the in-memory sqlite connection database and return a chart of active connections.",
        )
        .with_mime_type("application/json")
        .with_write(PERMIT_USER)
        .with_handler(move |r| chart_handler(&d, r, active_chart_sql)),
    )?;

    let d = db.clone();
    api.register_endpoint(
        Endpoint::new(
            "netquery/charts/bandwidth",
            "Bandwidth Chart",
            "Query the in-memory sqlite connection database and return a chart of bytes sent/received.",
        )
        .with_mime_type("application/json")
        .with_write(PERMIT_USER)
        .with_handler(move |r| chart_handler(&d, r, bandwidth_chart_sql)),
    )?;

    let d = db.clone();
    api.register_endpoint(
        Endpoint::new(
            "netquery/history/clear",
            "Remove connections from profile history",
            "Remove all connections from the history database for one or more profiles",
        )
        .with_mime_type("application/json")
        .with_write(PERMIT_USER)
        .with_action(move |r| history_clear(&d, r)),
    )?;

    let o = opts;
    api.register_endpoint(
        Endpoint::new(
            "netquery/history/cleanup",
            "Apply connection history retention threshold",
            "",
        )
        .with_write(PERMIT_USER)
        .with_action(move |_| {
            let errors = db
                .cleanup_history((o.now_unix)(), |p| (o.retention_days)(p))
                .map_err(|e| {
                    ApiError::Message(format!(
                        "failed to get a list of profiles from the history database: {e}"
                    ))
                })?;
            if errors.is_empty() {
                Ok("Deleted outdated connections.".to_string())
            } else {
                Err(ApiError::Message(multierror(&errors)))
            }
        }),
    )?;

    Ok(())
}

/// Formats errors like `go-multierror`.
fn multierror(errors: &[String]) -> String {
    let n = errors.len();
    let mut out = format!("{n} error{} occurred:", if n == 1 { "" } else { "s" });
    for e in errors {
        out.push_str("\n\t* ");
        out.push_str(e);
    }
    out.push_str("\n\n");
    out
}

fn history_clear(db: &Database, request: &mut Request) -> Result<String, ApiError> {
    let decode = |e: String| ApiError::Message(format!("failed to decode parameters in body: {e}"));
    let body = parse_json(&request.body).map_err(decode)?;
    let ids = match &body {
        Value::Null => Vec::new(),
        Value::Object(o) => match o.iter().find(|(k, _)| k.eq_ignore_ascii_case("profileIDs")) {
            None | Some((_, Value::Null)) => Vec::new(),
            Some((_, Value::Array(items))) => items
                .iter()
                .map(|v| match v {
                    Value::String(s) => Ok(s.clone()),
                    other => Err(decode(format!(
                        "json: cannot unmarshal {} into Go struct field .profileIDs of type string",
                        json_kind(other)
                    ))),
                })
                .collect::<Result<Vec<_>, _>>()?,
            Some((_, other)) => {
                return Err(decode(format!(
                    "json: cannot unmarshal {} into Go struct field .profileIDs of type []string",
                    json_kind(other)
                )))
            }
        },
        other => {
            return Err(decode(format!(
                "json: cannot unmarshal {} into Go value of type struct",
                json_kind(other)
            )))
        }
    };

    if ids.is_empty() {
        db.remove_all_history()
            .map_err(|e| ApiError::Message(format!("failed to remove all history: {e}")))?;
    } else {
        let mut errors = Vec::new();
        for id in &ids {
            if let Err(e) = db.remove_history_for_profile(id) {
                errors.push(format!("failed to clear history for {id:?}: {e}"));
            }
        }
        if !errors.is_empty() {
            return Err(ApiError::Message(multierror(&errors)));
        }
    }
    Ok("Successfully cleared history.".to_string())
}

fn json_kind(v: &Value) -> &'static str {
    match v {
        Value::Null => "null",
        Value::Bool(_) => "bool",
        Value::Number(_) => "number",
        Value::String(_) => "string",
        Value::Array(_) => "array",
        Value::Object(_) => "object",
    }
}

/// Like Go `json.Unmarshal`: an empty body is a syntax error.
fn parse_json(blob: &[u8]) -> Result<Value, String> {
    if blob.iter().all(u8::is_ascii_whitespace) {
        return Err("unexpected end of JSON input".to_string());
    }
    serde_json::from_slice(blob).map_err(|e| e.to_string())
}

/// Go `parseQueryRequestPayload`: the payload is the body of POST/PUT
/// requests or the `q` parameter of GET requests.
fn parse_payload(request: &Request) -> Result<Value, String> {
    let blob: &[u8] = match request.method.as_str() {
        "POST" | "PUT" => &request.body,
        "GET" => request.query_param("q").unwrap_or("").as_bytes(),
        _ => return Err("invalid HTTP method".to_string()),
    };
    parse_json(blob).map_err(|e| format!("invalid query: {e}"))
}

fn json_response(status: u16, value: &impl serde::Serialize) -> Response {
    match pm_dsd::gojson::to_vec_indent_unescaped(value, "", "  ") {
        Ok(body) => Response::body(status, body, TEXT_PLAIN),
        Err(e) => Response::error(500, &e.to_string()),
    }
}

/// Go encodes a nil slice as `null`.
fn rows_value(rows: Vec<Row>) -> Value {
    if rows.is_empty() {
        return Value::Null;
    }
    Value::Array(
        rows.into_iter()
            .map(|row| Value::Object(row.into_iter().collect()))
            .collect(),
    )
}

fn params_value(params: &BTreeMap<String, Param>) -> Value {
    Value::Object(
        params
            .iter()
            .map(|(k, p)| {
                let v = match p {
                    Param::Text(s) => Value::String(s.clone()),
                    Param::Float(f) => Value::from(*f),
                    Param::Bool(b) => Value::Bool(*b),
                };
                (k.clone(), v)
            })
            .collect(),
    )
}

fn query_handler(db: &Database, opts: &ApiOptions, request: &mut Request) -> Response {
    let payload = match parse_payload(request) {
        Ok(v) => v,
        Err(e) => return Response::error(400, &e),
    };
    let req = match QueryRequest::from_json(&payload) {
        Ok(r) => r,
        Err(e) => return Response::error(400, &format!("invalid query: {e}")),
    };
    let (sql, params) = match generate_sql(&req) {
        Ok(v) => v,
        Err(e) => return Response::error(400, &e.to_string()),
    };
    let rows = match db.query_raw(&sql, &params, true) {
        Ok(rows) => rows,
        Err(e) => return Response::error(500, &format!("{FAILED_QUERY}{e}")),
    };

    let mut body: BTreeMap<&str, Value> = BTreeMap::new();
    if (opts.dev_mode)() {
        body.insert("sql_prep_stmt", Value::String(sql));
        body.insert("sql_params", params_value(&params));
        body.insert("query", query_value(&req));
        body.insert("orderBy", order_by_value(&req));
        body.insert("groupBy", string_list_value(&req.group_by));
        body.insert("selects", selects_value(&req.select));
    }
    body.insert("results", rows_value(rows));
    json_response(200, &body)
}

fn batch_handler(db: &Database, request: &mut Request) -> Response {
    let payload = match parse_payload(request) {
        Ok(v) => v,
        Err(e) => return Response::error(400, &e),
    };
    let mut reqs = BTreeMap::new();
    match &payload {
        Value::Null => {}
        Value::Object(entries) => {
            for (key, value) in entries {
                match QueryRequest::from_json(value) {
                    Ok(r) => {
                        reqs.insert(key.clone(), r);
                    }
                    Err(e) => return Response::error(400, &format!("invalid query: {e}")),
                }
            }
        }
        other => {
            return Response::error(
                400,
                &format!(
                    "invalid query: json: cannot unmarshal {} into Go value of type netquery.BatchQueryRequestPayload",
                    json_kind(other)
                ),
            )
        }
    }

    let (results, errors) = match db.query_batch(&reqs) {
        Ok(v) => v,
        Err(DbError::Query(e)) => return Response::error(400, &e.to_string()),
        Err(e) => return Response::error(500, &e.to_string()),
    };

    let body: BTreeMap<String, Value> = results
        .into_iter()
        .map(|(key, rows)| (key, rows_value(rows)))
        .collect();
    let mut response = json_response(if errors.is_empty() { 200 } else { 500 }, &body);
    for e in &errors {
        let clean: String = e.chars().map(|c| if c.is_control() { ' ' } else { c }).collect();
        if let Ok(value) = HeaderValue::from_str(&clean) {
            response
                .headers
                .append(HeaderName::from_static("x-query-error"), value);
        }
    }
    response
}

/// SQL plus parameters; `None` params are encoded as `null` like Go's nil map.
type ChartSql = fn(&Value) -> Result<(String, Option<BTreeMap<String, Param>>), String>;

fn chart_handler(db: &Database, request: &mut Request, generate: ChartSql) -> Response {
    let payload = match parse_payload(request) {
        Ok(v) => v,
        Err(e) => return Response::error(400, &e),
    };
    let (sql, params) = match generate(&payload) {
        Ok(v) => v,
        Err(e) => return Response::error(400, &e),
    };
    let rows = match db.query_raw(&sql, &params.clone().unwrap_or_default(), true) {
        Ok(rows) => rows,
        Err(e) => return Response::error(500, &format!("{FAILED_QUERY}{e}")),
    };
    let body = BTreeMap::from([
        ("results", rows_value(rows)),
        ("query", Value::String(sql)),
        (
            "params",
            params.as_ref().map_or(Value::Null, params_value),
        ),
    ]);
    json_response(200, &body)
}

/// Keeps only the given payload keys, so that unrelated keys cannot fail
/// the parsing like they would not in Go.
fn pick(payload: &Value, keys: &[&str]) -> Value {
    let mut out = Map::new();
    if let Value::Object(o) = payload {
        for (k, v) in o {
            if keys.iter().any(|w| k.eq_ignore_ascii_case(w)) {
                out.insert(k.clone(), v.clone());
            }
        }
    }
    Value::Object(out)
}

fn active_chart_sql(payload: &Value) -> Result<(String, Option<BTreeMap<String, Param>>), String> {
    if !matches!(payload, Value::Object(_) | Value::Null) {
        return Err(format!(
            "invalid query: json: cannot unmarshal {} into Go value of type netquery.QueryActiveConnectionChartPayload",
            json_kind(payload)
        ));
    }
    let req = QueryRequest::from_json(&pick(payload, &["query", "textSearch"]))
        .map_err(|e| format!("invalid query: {e}"))?;
    let (clause, params) = chart_where_clause(&req).map_err(|e| multierror(&[e.to_string()]))?;
    let where_sql = if clause.is_empty() {
        String::new()
    } else {
        format!("WHERE ( {clause})")
    };
    let sql = format!(
        "\nWITH RECURSIVE epoch(x) AS (\n\tSELECT strftime('%s')-600\n\tUNION ALL\n\t\tSELECT x+1 FROM epoch WHERE x+1 < strftime('%s')+0\n)\nSELECT x as timestamp, SUM(verdict IN (2, 5, 6)) AS value, SUM(verdict NOT IN (2, 5, 6)) as countBlocked\n\tFROM epoch\n\tJOIN connections\n\t\tON strftime('%s', connections.started)+0 <= timestamp+0 AND (connections.ended IS NULL OR strftime('%s', connections.ended)+0 >= timestamp+0)\n\t{where_sql}\n\tGROUP BY round(timestamp/10, 0)*10;"
    );
    if clause.is_empty() {
        return Ok((sql, Some(BTreeMap::new())));
    }
    Ok((sql, Some(params)))
}

fn bandwidth_chart_sql(payload: &Value) -> Result<(String, Option<BTreeMap<String, Param>>), String> {
    let req = QueryRequest::from_json(&pick(payload, &["query", "groupBy"]))
        .map_err(|e| format!("invalid query: {e}"))?;
    let interval = match payload {
        Value::Object(o) => match o.iter().find(|(k, _)| k.eq_ignore_ascii_case("interval")) {
            None | Some((_, Value::Null)) => 0,
            Some((_, Value::Number(n))) => n.as_i64().ok_or_else(|| {
                format!("invalid query: json: cannot unmarshal number {n} into Go struct field BandwidthChartRequest.interval of type int")
            })?,
            Some((_, other)) => {
                return Err(format!(
                    "invalid query: json: cannot unmarshal {} into Go struct field BandwidthChartRequest.interval of type int",
                    json_kind(other)
                ))
            }
        },
        Value::Null => 0,
        other => {
            return Err(format!(
                "invalid query: json: cannot unmarshal {} into Go value of type netquery.BandwidthChartRequest",
                json_kind(other)
            ))
        }
    };
    let interval = if interval == 0 { 10 } else { interval };
    let interval_expr = format!("round(time/{interval}, 0)*{interval}");

    for gb in &req.group_by {
        if column(gb).is_none() {
            return Err(format!("unsupported groupBy key: {gb:?}"));
        }
    }

    let mut selects = vec![
        format!("{interval_expr} as timestamp"),
        "SUM(incoming) as incoming".to_string(),
        "SUM(outgoing) as outgoing".to_string(),
    ];
    selects.extend(req.group_by.iter().cloned());
    let mut group_by = vec![interval_expr];
    group_by.extend(req.group_by.iter().cloned());

    let (clause, params) = chart_where_clause(&req).map_err(|e| multierror(&[e.to_string()]))?;
    let where_sql = if clause.is_empty() {
        String::new()
    } else {
        format!("WHERE {clause}")
    };
    let sql = format!(
        "SELECT {}\n\t\tFROM main.bandwidth AS bw\n\t\tJOIN main.connections AS conns\n\t\t\tON bw.conn_id = conns.id\n\t\t{where_sql}\n\t\tGROUP BY {}\n\t\tORDER BY time ASC",
        selects.join(", "),
        group_by.join(", ")
    );
    // Go's empty query yields a nil parameter map.
    let params = if req.query.is_empty() { None } else { Some(params) };
    Ok((sql, params))
}

// Dev-mode echo of the parsed request, shaped like the Go structs.

fn string_list_value(list: &[String]) -> Value {
    if list.is_empty() {
        Value::Null
    } else {
        json!(list)
    }
}

fn order_by_value(req: &QueryRequest) -> Value {
    if req.order_by.is_empty() {
        return Value::Null;
    }
    Value::Array(
        req.order_by
            .iter()
            .map(|o| json!({"field": o.field, "desc": o.desc}))
            .collect(),
    )
}

fn matcher_value(m: &Matcher) -> Value {
    let mut o = Map::new();
    if let Some(v) = m.equal.as_ref().filter(|v| !v.is_null()) {
        o.insert("$eq".into(), v.clone());
    }
    if let Some(v) = m.not_equal.as_ref().filter(|v| !v.is_null()) {
        o.insert("$ne".into(), v.clone());
    }
    if let Some(v) = m.in_.as_ref().filter(|v| !v.is_empty()) {
        o.insert("$in".into(), Value::Array(v.clone()));
    }
    if let Some(v) = m.not_in.as_ref().filter(|v| !v.is_empty()) {
        o.insert("$notIn".into(), Value::Array(v.clone()));
    }
    if !m.like.is_empty() {
        o.insert("$like".into(), Value::String(m.like.clone()));
    }
    for (key, v) in [
        ("$gt", m.greater),
        ("$ge", m.greater_or_equal),
        ("$lt", m.less),
        ("$le", m.less_or_equal),
    ] {
        if let Some(f) = v {
            o.insert(key.into(), Value::from(f));
        }
    }
    Value::Object(o)
}

fn query_map_value(q: &crate::query::Query) -> Value {
    Value::Object(
        q.iter()
            .map(|(k, ms)| (k.clone(), Value::Array(ms.iter().map(matcher_value).collect())))
            .collect(),
    )
}

fn query_value(req: &QueryRequest) -> Value {
    if req.query.is_empty() {
        Value::Null
    } else {
        query_map_value(&req.query)
    }
}

fn selects_value(selects: &[Select]) -> Value {
    if selects.is_empty() {
        return Value::Null;
    }
    Value::Array(
        selects
            .iter()
            .map(|s| {
                let mut o = Map::new();
                o.insert("field".into(), json!(s.field));
                o.insert(
                    "$field".into(),
                    s.field_select
                        .as_ref()
                        .map_or(Value::Null, |(f, a)| json!({"field": f, "as": a})),
                );
                if let Some(c) = &s.count {
                    o.insert(
                        "$count".into(),
                        json!({"as": c.as_, "field": c.field, "distinct": c.distinct}),
                    );
                }
                if let Some(a) = &s.sum {
                    let cond = a.condition.as_ref().map_or(Value::Null, query_map_value);
                    o.insert(
                        "$sum".into(),
                        json!({"condition": cond, "field": a.field, "as": a.as_, "distinct": false}),
                    );
                }
                if let Some(a) = &s.min {
                    let mut m = Map::new();
                    if let Some(c) = &a.condition {
                        m.insert("condition".into(), query_map_value(c));
                    }
                    m.insert("field".into(), json!(a.field));
                    m.insert("as".into(), json!(a.as_));
                    m.insert("distinct".into(), json!(false));
                    o.insert("$min".into(), Value::Object(m));
                }
                if let Some(d) = &s.distinct {
                    o.insert("$distinct".into(), json!(d));
                }
                Value::Object(o)
            })
            .collect(),
    )
}
