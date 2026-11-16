//! Compares the SQL generator against Go output (`tools/rust-fixtures`).

use pm_netquery::{generate_sql, schema, Param, QueryRequest};
use serde_json::Value;

fn fixture() -> Value {
    let raw = include_str!("../../fixtures/m0/netquery.json");
    serde_json::from_str(raw).expect("fixture json")
}

#[test]
fn create_statements_match_go() {
    let f = fixture();
    assert_eq!(schema::create_statement("main", true), f["create_main"].as_str().unwrap());
    assert_eq!(schema::create_statement("history", false), f["create_history"].as_str().unwrap());
}

fn param_json(p: &Param) -> Value {
    match p {
        Param::Text(s) => serde_json::json!({"type": "string", "value": s}),
        Param::Float(f) => serde_json::json!({"type": "float64", "value": f}),
        Param::Bool(b) => serde_json::json!({"type": "bool", "value": b}),
    }
}

fn same_number(a: &Value, b: &Value) -> bool {
    match (a.as_f64(), b.as_f64()) {
        (Some(x), Some(y)) => x == y,
        _ => a == b,
    }
}

#[test]
fn queries_match_go() {
    let f = fixture();
    let cases = f["cases"].as_array().unwrap();
    assert!(cases.len() > 30);
    for case in cases {
        let raw = case["request"].as_str().unwrap();
        let req: Value = serde_json::from_str(raw).unwrap();
        let result = QueryRequest::from_json(&req).and_then(|r| generate_sql(&r));

        // Deliberate deviation: Go's alias regex is unanchored and lets this through.
        if raw.contains(r#""as":"a b""#) {
            assert!(result.is_err(), "{raw}: alias with a space must be rejected");
            continue;
        }

        if case.get("error").is_some() || case.get("unmarshal_error").is_some() {
            assert!(result.is_err(), "{raw}: expected error, got {result:?}");
            continue;
        }
        let (sql, params) = result.unwrap_or_else(|e| panic!("{raw}: {e}"));
        assert_eq!(sql, case["sql"].as_str().unwrap(), "{raw}");

        let want = case["params"].as_object().unwrap();
        assert_eq!(params.len(), want.len(), "{raw}: {params:?}");
        for (k, p) in &params {
            let got = param_json(p);
            let exp = &want[k];
            assert_eq!(got["type"], exp["type"], "{raw}: {k}");
            assert!(same_number(&got["value"], &exp["value"]), "{raw}: {k}: {got} vs {exp}");
        }
    }
}

#[test]
fn field_select_is_validated_next_to_count() {
    let raw = r#"{"select":[{"$count":{"field":"*"},"$field":{"field":"1); DROP TABLE x; --","as":"x"}}]}"#;
    let req = QueryRequest::from_json(&serde_json::from_str(raw).unwrap()).unwrap();
    assert!(generate_sql(&req).is_err());
}
