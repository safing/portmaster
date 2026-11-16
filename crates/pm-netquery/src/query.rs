//! JSON query → SQL, port of Go `netquery/query.go` and `query_request.go`.
//!
//! Deliberate deviations from Go (security): `as` aliases must be letters only
//! (Go's regex is unanchored and lets any text into the SQL) and `databases`
//! may only name `main` or `history` (Go interpolates them unchecked).

use std::collections::BTreeMap;
use std::sync::LazyLock;

use regex::Regex;
use serde_json::{Map, Value};

use crate::schema::{column, ColumnKind};
use crate::timefmt::{format_sqlite, parse_rfc3339};

static CHAR_ONLY: LazyLock<Regex> = LazyLock::new(|| Regex::new("^[a-zA-Z]+$").expect("regex"));

/// A query error; texts follow Go where practical.
#[derive(Debug, Clone, PartialEq, Eq, thiserror::Error)]
pub enum QueryError {
    /// The payload JSON does not have the expected shape.
    #[error("{0}")]
    Unmarshal(String),
    #[error("{0}")]
    Invalid(String),
}

fn invalid(msg: impl Into<String>) -> QueryError {
    QueryError::Invalid(msg.into())
}

fn unmarshal(msg: impl Into<String>) -> QueryError {
    QueryError::Unmarshal(msg.into())
}

/// A bound SQL parameter.
#[derive(Debug, Clone, PartialEq)]
pub enum Param {
    Text(String),
    Float(f64),
    Bool(bool),
}

/// One condition on a column (Go `Matcher`).
#[derive(Debug, Clone, Default, PartialEq)]
pub struct Matcher {
    pub equal: Option<Value>,
    pub not_equal: Option<Value>,
    pub in_: Option<Vec<Value>>,
    pub not_in: Option<Vec<Value>>,
    pub like: String,
    pub greater: Option<f64>,
    pub greater_or_equal: Option<f64>,
    pub less: Option<f64>,
    pub less_or_equal: Option<f64>,
}

/// Column name → alternatives (OR); columns are combined with AND.
pub type Query = BTreeMap<String, Vec<Matcher>>;

#[derive(Debug, Clone, Default, PartialEq)]
pub struct Count {
    pub as_: String,
    pub field: String,
    pub distinct: bool,
}

#[derive(Debug, Clone, Default, PartialEq)]
pub struct Aggregate {
    pub condition: Option<Query>,
    pub field: String,
    pub as_: String,
}

#[derive(Debug, Clone, Default, PartialEq)]
pub struct Select {
    pub field: String,
    pub field_select: Option<(String, String)>,
    pub count: Option<Count>,
    pub sum: Option<Aggregate>,
    pub min: Option<Aggregate>,
    pub distinct: Option<String>,
}

#[derive(Debug, Clone, Default, PartialEq)]
pub struct OrderBy {
    pub field: String,
    pub desc: bool,
}

#[derive(Debug, Clone, Default, PartialEq)]
pub struct TextSearch {
    pub fields: Vec<String>,
    pub value: String,
}

/// A netquery request payload (Go `QueryRequestPayload`).
#[derive(Debug, Clone, Default, PartialEq)]
pub struct QueryRequest {
    pub select: Vec<Select>,
    pub query: Query,
    pub order_by: Vec<OrderBy>,
    pub group_by: Vec<String>,
    pub text_search: Option<TextSearch>,
    pub databases: Vec<String>,
    pub page_size: i64,
    pub page: i64,
}

// ---------------------------------------------------------------- parsing

fn type_err(field: &str, want: &str, got: &Value) -> QueryError {
    let kind = match got {
        Value::Null => "null",
        Value::Bool(_) => "bool",
        Value::Number(_) => "number",
        Value::String(_) => "string",
        Value::Array(_) => "array",
        Value::Object(_) => "object",
    };
    unmarshal(format!("json: cannot unmarshal {kind} into Go struct field {field} of type {want}"))
}

fn as_string(v: &Value, field: &str) -> Result<String, QueryError> {
    match v {
        Value::String(s) => Ok(s.clone()),
        Value::Null => Ok(String::new()),
        other => Err(type_err(field, "string", other)),
    }
}

fn as_bool(v: &Value, field: &str) -> Result<bool, QueryError> {
    match v {
        Value::Bool(b) => Ok(*b),
        Value::Null => Ok(false),
        other => Err(type_err(field, "bool", other)),
    }
}

fn as_float(v: &Value, field: &str) -> Result<Option<f64>, QueryError> {
    match v {
        Value::Null => Ok(None),
        Value::Number(n) => Ok(n.as_f64()),
        other => Err(type_err(field, "float64", other)),
    }
}

fn as_int(v: &Value, field: &str) -> Result<i64, QueryError> {
    match v {
        Value::Null => Ok(0),
        Value::Number(n) => n.as_i64().ok_or_else(|| type_err(field, "int", v)),
        other => Err(type_err(field, "int", other)),
    }
}

fn as_list(v: &Value, field: &str) -> Result<Option<Vec<Value>>, QueryError> {
    match v {
        Value::Null => Ok(None),
        Value::Array(a) => Ok(Some(a.clone())),
        other => Err(type_err(field, "[]interface {}", other)),
    }
}

fn as_object<'a>(v: &'a Value, field: &str) -> Result<Option<&'a Map<String, Value>>, QueryError> {
    match v {
        Value::Null => Ok(None),
        Value::Object(o) => Ok(Some(o)),
        other => Err(type_err(field, "struct", other)),
    }
}

/// Go `parseMatcher`: decode then require at least one condition.
fn parse_matcher(obj: &Map<String, Value>) -> Result<Matcher, QueryError> {
    let mut m = Matcher::default();
    for (key, v) in obj {
        match key.as_str() {
            "$eq" => m.equal = (!v.is_null()).then(|| v.clone()),
            "$ne" => m.not_equal = (!v.is_null()).then(|| v.clone()),
            "$in" => m.in_ = as_list(v, "Matcher.$in")?,
            "$notIn" => m.not_in = as_list(v, "Matcher.$notIn")?,
            "$like" => m.like = as_string(v, "Matcher.$like")?,
            "$gt" => m.greater = as_float(v, "Matcher.$gt")?,
            "$ge" => m.greater_or_equal = as_float(v, "Matcher.$ge")?,
            "$lt" => m.less = as_float(v, "Matcher.$lt")?,
            "$le" => m.less_or_equal = as_float(v, "Matcher.$le")?,
            _ => {}
        }
    }
    let found = m.equal.is_some()
        || m.not_equal.is_some()
        || m.in_.is_some()
        || m.not_in.is_some()
        || !m.like.is_empty()
        || m.greater.is_some()
        || m.greater_or_equal.is_some()
        || m.less.is_some()
        || m.less_or_equal.is_some();
    if !found {
        return Err(unmarshal("invalid query matcher: no conditions specified"));
    }
    Ok(m)
}

/// Go `Query.UnmarshalJSON`.
pub fn parse_query(v: &Value) -> Result<Query, QueryError> {
    let mut query = Query::new();
    let Some(obj) = as_object(v, "query")? else {
        return Ok(query);
    };
    for (column, raw) in obj {
        let matchers = match raw {
            Value::Object(o) => vec![parse_matcher(o)?],
            Value::Array(items) => {
                let mut out = Vec::with_capacity(items.len());
                for item in items {
                    match item {
                        Value::Object(o) => out.push(parse_matcher(o)?),
                        Value::Array(_) => {
                            return Err(unmarshal(format!("invalid token [ in query for column {column}")))
                        }
                        Value::Null => out.push(Matcher::default()),
                        primitive => out.push(Matcher {
                            equal: Some(primitive.clone()),
                            ..Matcher::default()
                        }),
                    }
                }
                out
            }
            Value::Null => vec![Matcher::default()],
            primitive => vec![Matcher {
                equal: Some(primitive.clone()),
                ..Matcher::default()
            }],
        };
        query.insert(column.clone(), matchers);
    }
    Ok(query)
}

fn check_as(alias: &str, what: &str) -> Result<(), QueryError> {
    if !alias.is_empty() && !CHAR_ONLY.is_match(alias) {
        return Err(unmarshal(format!(
            "invalid characters in {what}.as, value must match [a-zA-Z]+"
        )));
    }
    Ok(())
}

fn parse_aggregate(v: &Value, what: &str) -> Result<Option<Aggregate>, QueryError> {
    let Some(o) = as_object(v, what)? else {
        return Ok(None);
    };
    let mut agg = Aggregate::default();
    for (k, v) in o {
        match k.as_str() {
            "condition" if !v.is_null() => agg.condition = Some(parse_query(v)?),
            "field" => agg.field = as_string(v, &format!("{what}.field"))?,
            "as" => agg.as_ = as_string(v, &format!("{what}.as"))?,
            _ => {}
        }
    }
    check_as(&agg.as_, what)?;
    Ok(Some(agg))
}

fn parse_select(v: &Value) -> Result<Select, QueryError> {
    let obj = match v {
        Value::Object(o) => o,
        Value::String(s) => {
            return Ok(Select {
                field: s.clone(),
                ..Select::default()
            })
        }
        other => return Err(type_err("Select", "string", other)),
    };
    let mut sel = Select::default();
    for (k, v) in obj {
        match k.as_str() {
            "field" => sel.field = as_string(v, "field")?,
            "$count" => {
                if let Some(o) = as_object(v, "$count")? {
                    let mut c = Count::default();
                    for (k, v) in o {
                        match k.as_str() {
                            "as" => c.as_ = as_string(v, "Count.as")?,
                            "field" => c.field = as_string(v, "Count.field")?,
                            "distinct" => c.distinct = as_bool(v, "Count.distinct")?,
                            _ => {}
                        }
                    }
                    check_as(&c.as_, "$count")?;
                    sel.count = Some(c);
                }
            }
            "$sum" => sel.sum = parse_aggregate(v, "$sum")?,
            "$min" => sel.min = parse_aggregate(v, "$min")?,
            "$distinct" => {
                sel.distinct = match v {
                    Value::Null => None,
                    other => Some(as_string(other, "$distinct")?),
                }
            }
            "$field" => {
                if let Some(o) = as_object(v, "$field")? {
                    let field = o.get("field").map_or(Ok(String::new()), |v| as_string(v, "FieldSelect.field"))?;
                    let alias = o.get("as").map_or(Ok(String::new()), |v| as_string(v, "FieldSelect.as"))?;
                    check_as(&alias, "$field")?;
                    sel.field_select = Some((field, alias));
                }
            }
            _ => {}
        }
    }
    Ok(sel)
}

fn parse_order_by(v: &Value) -> Result<OrderBy, QueryError> {
    match v {
        Value::Object(o) => Ok(OrderBy {
            field: o.get("field").map_or(Ok(String::new()), |v| as_string(v, "OrderBy.field"))?,
            desc: o.get("desc").map_or(Ok(false), |v| as_bool(v, "OrderBy.desc"))?,
        }),
        other => Ok(OrderBy {
            field: as_string(other, "OrderBy")?,
            desc: false,
        }),
    }
}

impl QueryRequest {
    /// Parses a payload like Go `json.Unmarshal` into `QueryRequestPayload`.
    pub fn from_json(v: &Value) -> Result<QueryRequest, QueryError> {
        let obj = as_object(v, "QueryRequestPayload")?.cloned().unwrap_or_default();
        let mut req = QueryRequest::default();
        for (k, v) in &obj {
            match k.as_str() {
                "select" => {
                    req.select = match v {
                        Value::Array(items) => items.iter().map(parse_select).collect::<Result<_, _>>()?,
                        Value::Object(_) => vec![parse_select(v)?],
                        // Go decodes a plain string here and then drops it.
                        Value::String(_) | Value::Null => Vec::new(),
                        other => return Err(type_err("select", "string", other)),
                    }
                }
                "query" => req.query = parse_query(v)?,
                "orderBy" => {
                    req.order_by = match v {
                        Value::Array(items) => items.iter().map(parse_order_by).collect::<Result<_, _>>()?,
                        Value::Null => Vec::new(),
                        other => vec![parse_order_by(other)?],
                    }
                }
                "groupBy" => {
                    req.group_by = as_list(v, "groupBy")?
                        .unwrap_or_default()
                        .iter()
                        .map(|g| as_string(g, "groupBy"))
                        .collect::<Result<_, _>>()?
                }
                "textSearch" => {
                    if let Some(o) = as_object(v, "textSearch")? {
                        let fields = o
                            .get("fields")
                            .map_or(Ok(None), |f| as_list(f, "TextSearch.fields"))?
                            .unwrap_or_default()
                            .iter()
                            .map(|f| as_string(f, "TextSearch.fields"))
                            .collect::<Result<_, _>>()?;
                        let value = o.get("value").map_or(Ok(String::new()), |v| as_string(v, "TextSearch.value"))?;
                        req.text_search = Some(TextSearch { fields, value });
                    }
                }
                "databases" => {
                    req.databases = as_list(v, "databases")?
                        .unwrap_or_default()
                        .iter()
                        .map(|d| as_string(d, "databases"))
                        .collect::<Result<_, _>>()?
                }
                "pageSize" => req.page_size = as_int(v, "pageSize")?,
                "page" => req.page = as_int(v, "page")?,
                _ => {}
            }
        }
        Ok(req)
    }
}

// ---------------------------------------------------------------- SQL

#[derive(Default)]
struct Builder {
    selected: Vec<String>,
    whitelisted: Vec<String>,
    params: BTreeMap<String, Param>,
}

impl Builder {
    fn validate_column(&self, field: &str) -> Result<String, QueryError> {
        if let Some(col) = column(field) {
            return Ok(col.name.to_string());
        }
        if self.whitelisted.iter().any(|f| f == field) || self.selected.iter().any(|f| f == field) {
            return Ok(field.to_string());
        }
        Err(invalid(format!("column name {field:?} not allowed")))
    }
}

fn is_number(v: &Value) -> bool {
    v.is_number()
}

/// Go `orm.EncodeValue` with the default datetime encoder (UTC).
fn encode(col: &crate::schema::Column, v: &Value) -> Result<Param, QueryError> {
    match v {
        Value::String(s) if col.is_time => {
            let unix = parse_rfc3339(s).ok_or_else(|| {
                invalid(format!("failed to encode {s} for column {}: failed to parse time as RFC3339", col.name))
            })?;
            Ok(Param::Text(format_sqlite(unix)))
        }
        Value::Number(n) if col.is_time && col.kind == ColumnKind::Integer => {
            Ok(Param::Float(n.as_f64().unwrap_or_default().trunc()))
        }
        Value::String(s) => Ok(Param::Text(s.clone())),
        Value::Number(n) => Ok(Param::Float(n.as_f64().unwrap_or_default())),
        Value::Bool(b) => Ok(Param::Bool(*b)),
        other => Err(invalid(format!(
            "failed to encode {other} for column {}: cannot convert value for use in SQLite",
            col.name
        ))),
    }
}

/// Go `Matcher.toSQLConditionClause`.
fn matcher_clause(
    m: &Matcher,
    suffix: &str,
    col: &crate::schema::Column,
    params: &mut BTreeMap<String, Param>,
) -> Result<String, QueryError> {
    let key = format!("{}{suffix}", col.name);
    let mut parts = Vec::new();
    let mut add = |op: &str, op_suffix: &str, list: bool, values: &[Value]| -> Result<(), QueryError> {
        let mut placeholders = Vec::new();
        for (idx, value) in values.iter().enumerate() {
            let encoded = if col.is_time && col.kind == ColumnKind::Text && is_number(value) {
                Param::Float(value.as_f64().unwrap_or_default())
            } else {
                encode(col, value)?
            };
            let uniq = format!(":{key}{op_suffix}{idx}");
            placeholders.push(uniq.clone());
            params.insert(uniq, encoded);
        }
        let mut name = col.name.to_string();
        if values.first().is_some_and(is_number) && col.is_time && col.kind == ColumnKind::Text {
            name = format!("strftime('%s', {name})+0");
        }
        if placeholders.len() == 1 && !list {
            parts.push(format!("{name} {op} {}", placeholders[0]));
        } else {
            parts.push(format!("{name} {op} ( {} )", placeholders.join(", ")));
        }
        Ok(())
    };

    if let Some(v) = &m.equal {
        add("=", "eq", false, std::slice::from_ref(v))?;
    }
    if let Some(v) = &m.not_equal {
        add("!=", "ne", false, std::slice::from_ref(v))?;
    }
    if let Some(v) = &m.in_ {
        add("IN", "in", true, v)?;
    }
    if let Some(v) = &m.not_in {
        add("NOT IN", "notin", true, v)?;
    }
    if !m.like.is_empty() {
        add("LIKE", "like", false, &[Value::String(m.like.clone())])?;
    }
    let num = |f: f64| serde_json::Number::from_f64(f).map_or(Value::Null, Value::Number);
    if let Some(f) = m.greater {
        add(">", "gt", false, &[num(f)])?;
    }
    if let Some(f) = m.greater_or_equal {
        add(">=", "ge", false, &[num(f)])?;
    }
    if let Some(f) = m.less {
        add("<", "lt", false, &[num(f)])?;
    }
    if let Some(f) = m.less_or_equal {
        add("<=", "le", false, &[num(f)])?;
    }

    Ok(match parts.len() {
        0 => "( 1 = 1 )".to_string(),
        1 => parts.remove(0),
        _ => format!("( {} )", parts.join(" AND ")),
    })
}

/// Go `Query.toSQLWhereClause` (columns sorted by name).
fn where_clause(query: &Query, suffix: &str, params: &mut BTreeMap<String, Param>) -> Result<String, QueryError> {
    let mut stmts = Vec::with_capacity(query.len());
    for (name, matchers) in query {
        let col = column(name).ok_or_else(|| invalid(format!("column {name} is not allowed")))?;
        let mut parts = Vec::with_capacity(matchers.len());
        for (idx, m) in matchers.iter().enumerate() {
            parts.push(matcher_clause(m, &format!("{suffix}{idx}"), col, params)?);
        }
        stmts.push(format!("( {} )", parts.join(" OR ")));
    }
    Ok(stmts.join(" AND "))
}

impl Builder {
    fn prepare_selected(&mut self, req: &QueryRequest) -> Result<(), QueryError> {
        for (idx, s) in req.select.iter().enumerate() {
            let field = if let Some(c) = &s.count {
                c.field.clone()
            } else if let Some(d) = &s.distinct {
                d.clone()
            } else if let Some(a) = &s.sum {
                if a.field.is_empty() { "*".to_string() } else { a.field.clone() }
            } else if let Some(a) = &s.min {
                if a.field.is_empty() { "*".to_string() } else { a.field.clone() }
            } else if let Some((f, _)) = &s.field_select {
                f.clone()
            } else {
                s.field.clone()
            };

            let mut col = "*".to_string();
            if field != "*" || (s.count.is_none() && s.sum.is_none()) {
                col = self.validate_column(&field)?;
            }

            if let Some((f, alias)) = &s.field_select {
                let alias = if alias.is_empty() { f.clone() } else { alias.clone() };
                self.selected.push(format!("{f} AS {alias}"));
                self.whitelisted.push(alias);
            } else if let Some(c) = &s.count {
                let alias = if c.as_.is_empty() { format!("{col}_count") } else { c.as_.clone() };
                let distinct = if c.distinct { "DISTINCT " } else { "" };
                self.selected.push(format!("COUNT({distinct}{col}) AS {alias}"));
                self.whitelisted.push(alias);
            } else if let Some(a) = &s.sum {
                self.aggregate("SUM", "$sum", a, &field, idx)?;
            } else if let Some(a) = &s.min {
                self.aggregate("MIN", "$min", a, &field, idx)?;
            } else if s.distinct.is_some() {
                self.selected.push(format!("DISTINCT {col}"));
                self.whitelisted.push(col);
            } else {
                self.selected.push(col);
            }
        }
        Ok(())
    }

    fn aggregate(&mut self, func: &str, what: &str, a: &Aggregate, field: &str, idx: usize) -> Result<(), QueryError> {
        if a.as_.is_empty() {
            return Err(invalid(format!("missing 'as' for {what}")));
        }
        let clause = if !a.field.is_empty() {
            field.to_string()
        } else {
            let empty = Query::new();
            let cond = a.condition.as_ref().unwrap_or(&empty);
            where_clause(cond, &format!("sel{idx}"), &mut self.params)
                .map_err(|e| invalid(format!("in {what}: {e}")))?
        };
        self.selected.push(format!("{func}({clause}) AS {}", a.as_));
        self.whitelisted.push(a.as_.clone());
        Ok(())
    }
}

/// Go `QueryRequestPayload.generateSQL` for the connections table.
pub fn generate_sql(req: &QueryRequest) -> Result<(String, BTreeMap<String, Param>), QueryError> {
    let mut b = Builder::default();
    b.prepare_selected(req)
        .map_err(|e| invalid(format!("perparing selected fields: {e}")))?;

    let mut params = std::mem::take(&mut b.params);
    let mut where_ = where_clause(&req.query, "", &mut params)
        .map_err(|e| invalid(format!("generating where clause: {e}")))?;

    if let Some(text) = &req.text_search {
        let key = ":t".to_string();
        let mut parts = Vec::new();
        for f in &text.fields {
            let col = column(f).ok_or_else(|| {
                invalid(format!("generating text-search clause: column {f} is not allowed in text-search"))
            })?;
            if col.kind != ColumnKind::Text {
                return Err(invalid(format!(
                    "generating text-search clause: type of column {} cannot be used in text-search",
                    col.name
                )));
            }
            parts.push(format!("{} LIKE {key}", col.name));
        }
        if !parts.is_empty() {
            params.insert(key, Param::Text(format!("%{}%", text.value)));
            if !where_.is_empty() {
                where_ += " AND ";
            }
            where_ += &format!("( {} )", parts.join(" OR "));
        }
    }

    let group_by = if req.group_by.is_empty() {
        String::new()
    } else {
        let cols = req
            .group_by
            .iter()
            .map(|g| b.validate_column(g))
            .collect::<Result<Vec<_>, _>>()
            .map_err(|e| invalid(format!("generating group-by clause: {e}")))?;
        if b.selected.is_empty() {
            b.selected.extend(cols.iter().cloned());
        }
        format!("GROUP BY {}", cols.join(", "))
    };

    let order_by = if req.order_by.is_empty() {
        String::new()
    } else {
        let parts = req
            .order_by
            .iter()
            .map(|o| {
                b.validate_column(&o.field)
                    .map(|c| format!("{c} {}", if o.desc { "DESC" } else { "ASC" }))
            })
            .collect::<Result<Vec<_>, _>>()
            .map_err(|e| invalid(format!("generating order-by clause: {e}")))?;
        format!("ORDER BY {}", parts.join(", "))
    };

    let select = if b.selected.is_empty() {
        "*".to_string()
    } else {
        b.selected.join(", ")
    };
    if !where_.is_empty() {
        where_ = format!("WHERE {where_}");
    }

    let databases = if req.databases.is_empty() {
        vec!["main".to_string()]
    } else {
        req.databases.clone()
    };
    for db in &databases {
        if db != "main" && db != "history" {
            return Err(invalid(format!("database {db:?} not allowed")));
        }
    }
    let sources: Vec<String> = databases
        .iter()
        .map(|db| format!("SELECT * FROM {db}.connections {where_}"))
        .collect();

    let mut limit = req.page_size;
    if limit <= 0 || limit > 100 {
        limit = 100;
    }
    let mut paging = format!("LIMIT {limit}");
    if req.page > 0 {
        paging += &format!(" OFFSET {}", req.page * limit);
    }

    let sql = format!(
        "SELECT {select} FROM ( {} )  {group_by} {order_by} {paging}",
        sources.join(" UNION ")
    );
    Ok((sql.trim().to_string(), params))
}
