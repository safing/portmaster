use pm_record::accessor::JsonAccessor;
use serde_json::{json, Value};

fn acc() -> JsonAccessor {
    JsonAccessor::new(json!({
        "S": "banana", "A": ["black", "white"],
        "I": 42, "I8": 42, "I16": 42, "I32": 42, "I64": 42,
        "UI": 42, "UI8": 42, "UI16": 42, "UI32": 42, "UI64": 42,
        "F32": 42.42, "F64": 42.42, "B": true
    }))
}

const INTS: [&str; 10] = [
    "I", "I8", "I16", "I32", "I64", "UI", "UI8", "UI16", "UI32", "UI64",
];

fn check_get(a: &JsonAccessor, int: i64, float: f64, s: &str, arr: [&str; 2], b: bool) {
    assert_eq!(a.get_string("S").as_deref(), Some(s));
    assert_eq!(
        a.get_string_array("A"),
        Some(arr.map(String::from).to_vec())
    );
    for k in INTS {
        assert_eq!(a.get_int(k), Some(int), "{k}");
    }
    for k in ["F32", "F64"] {
        assert_eq!(a.get_float(k).unwrap() as i64, float as i64);
    }
    assert_eq!(a.get_bool("B"), Some(b));
}

#[test]
fn accessor_table() {
    let mut a = acc();
    check_get(&a, 42, 42.42, "banana", ["black", "white"], true);

    a.set("S", json!("coconut")).unwrap();
    a.set("A", json!(["green", "blue"])).unwrap();
    for k in INTS {
        a.set(k, json!(44)).unwrap();
    }
    a.set("F32", json!(44.44)).unwrap();
    a.set("F64", json!(44.44)).unwrap();
    a.set("B", json!(false)).unwrap();
    check_get(&a, 44, 44.44, "coconut", ["green", "blue"], false);

    let bad: Vec<(&str, Value)> = vec![
        ("S", json!(true)),
        ("S", json!(false)),
        ("S", json!(1)),
        ("S", json!(1.1)),
        ("A", json!("1")),
        ("A", json!(true)),
        ("A", json!(false)),
        ("A", json!(1)),
        ("A", json!(1.1)),
        ("I", json!("1")),
        ("I8", json!("1")),
        ("UI64", json!("1")),
        ("F32", json!("1.1")),
        ("F64", json!("1.1")),
        ("B", json!("false")),
        ("B", json!(1)),
        ("B", json!(1.1)),
    ];
    for (k, v) in bad {
        assert!(a.set(k, v.clone()).is_err(), "{k} <- {v}");
    }
    check_get(&a, 44, 44.44, "coconut", ["green", "blue"], false);

    for k in ["S", "A", "F32", "B"].into_iter().chain(INTS) {
        assert!(a.exists(k), "{k}");
    }
    assert!(!a.exists("X"));
}

#[test]
fn wrong_type_getters_fail() {
    let a = acc();
    assert_eq!(a.get_string("I"), None);
    assert_eq!(a.get_int("S"), None);
    assert_eq!(a.get_float("B"), None);
    assert_eq!(a.get_bool("S"), None);
    assert_eq!(a.get_string_array("X"), None);
    assert_eq!(a.get("X"), None);
    assert_eq!(a.get("S"), Some(json!("banana")));
}

#[test]
fn paths() {
    let mut a = JsonAccessor::new(json!({
        "Nested": {"a": "x", "b.c": 1, "k": [10, {"z": "deep"}]},
        "Tags": ["a", "b|c"], "plain.key": "dotted", "1": "numkey", "n": null
    }));
    assert_eq!(a.get_string("Nested.a").as_deref(), Some("x"));
    assert_eq!(a.get_int(r"Nested.b\.c"), Some(1));
    assert_eq!(a.get_string(r"plain\.key").as_deref(), Some("dotted"));
    assert_eq!(a.get_string("plain.key"), None);
    assert_eq!(a.get_string("Tags.1").as_deref(), Some("b|c"));
    assert_eq!(a.get_string("Tags.2"), None);
    assert_eq!(a.get_int("Nested.k.0"), Some(10));
    assert_eq!(a.get_string("Nested.k.1.z").as_deref(), Some("deep"));
    assert_eq!(a.get_int("Tags.#"), Some(2));
    assert_eq!(a.get_int("Nested.k.#"), Some(2));
    assert_eq!(a.get_string("1").as_deref(), Some("numkey"));
    assert!(a.exists("n"));
    assert!(!a.exists(""));
    assert!(!a.exists("Tags.x"));
    assert!(!a.exists("Tags.-1"));

    a.set("Nested.a", json!("y")).unwrap();
    assert!(a.set("Nested.a", json!(1)).is_err());
    a.set("New.deep.key", json!(true)).unwrap();
    assert_eq!(a.get_bool("New.deep.key"), Some(true));
    a.set("Tags.2", json!("c")).unwrap();
    assert_eq!(a.get_string("Tags.2").as_deref(), Some("c"));
    a.set("Tags.0", json!("A")).unwrap();
    assert!(a.set("Tags.0", json!(5)).is_err());
    assert!(a.set("Tags.x", json!("v")).is_err());
    assert!(a.set("Nested.a.b", json!("v")).is_err());
    assert!(a.set("", json!("v")).is_err());
}

#[test]
fn from_bytes_and_get_string_array_quirks() {
    let a = JsonAccessor::from_slice(br#"{"s":"one","m":["a",1],"n":null}"#).unwrap();
    // gjson wraps non-array values into a one element array.
    assert_eq!(a.get_string_array("s"), Some(vec!["one".to_string()]));
    assert_eq!(a.get_string_array("m"), None);
    assert_eq!(a.get_string_array("n"), Some(vec![]));
    assert!(JsonAccessor::from_slice(b"{").is_err());
}
