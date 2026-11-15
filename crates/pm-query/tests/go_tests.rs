//! Ports of the Go `base/database/query` unit tests.
//!
//! Two Go tests could not be ported literally:
//! * `TestQuery` uses a gjson test document that is not valid JSON (gjson is
//!   lenient, serde_json is not); the two malformed spots are repaired here.
//! * Go's constructor tests reach into unexported `newXCondition` helpers; the
//!   equivalent behavior is exercised through `Condition::new`, the public API.

use pm_query::{
    extract_snippets, parse_query, Condition, Query, CONTAINS, ENDS_WITH, EQUALS, EXISTS,
    FLOAT_EQUALS, FLOAT_GREATER_THAN, FLOAT_GREATER_THAN_OR_EQUAL, FLOAT_LESS_THAN,
    FLOAT_LESS_THAN_OR_EQUAL, GREATER_THAN, GREATER_THAN_OR_EQUAL, IN, IS, LESS_THAN,
    LESS_THAN_OR_EQUAL, MATCHES, SAME_AS, STARTS_WITH,
};
use pm_record::{Key, Meta, Record};

#[test]
fn extract_snippets_matches_go() {
    let text = r#"query test: where ( "bananas" > 100 and monkeys.# <= "12")or(coconuts < 10 "and" area > 50) or name sameas Julian or name matches ^King\ "#;
    let expected: &[(&str, usize)] = &[
        ("query", 1),
        ("test:", 7),
        ("where", 13),
        ("(", 19),
        ("bananas", 21),
        (">", 31),
        ("100", 33),
        ("and", 37),
        ("monkeys.#", 41),
        ("<=", 51),
        ("12", 54),
        (")", 58),
        ("or", 59),
        ("(", 61),
        ("coconuts", 62),
        ("<", 71),
        ("10", 73),
        ("and", 76),
        ("area", 82),
        (">", 87),
        ("50", 89),
        (")", 91),
        ("or", 93),
        ("name", 96),
        ("sameas", 101),
        ("Julian", 108),
        ("or", 115),
        ("name", 118),
        ("matches", 123),
        ("^King ", 131),
    ];

    let snippets = extract_snippets(text).unwrap();
    let got: Vec<(String, usize)> = snippets
        .iter()
        .map(|s| (s.text_lossy(), s.position))
        .collect();
    let expected: Vec<(String, usize)> = expected
        .iter()
        .map(|(text, position)| ((*text).to_string(), *position))
        .collect();
    assert_eq!(got, expected);
}

fn parsed(text: &str) -> Query {
    parse_query(text).unwrap_or_else(|err| panic!("{text:?}: {err}"))
}

fn assert_round_trip(text: &str, expected: Query) {
    let expected = expected.check().unwrap();
    let got = parsed(text);
    assert_eq!(got.print(), text, "print mismatch");
    assert_eq!(got, expected, "structure mismatch for {text:?}");
}

#[test]
fn parse_query_matches_go() {
    assert_round_trip(
        r#"query test: where (bananas > 100 and monkeys.# <= 12) or not (coconuts < 10 and area not > 50) or name sameas Julian or name matches "^King " orderby name limit 10 offset 20"#,
        Query::new("test:")
            .where_(Condition::or(vec![
                Condition::and(vec![
                    Condition::new("bananas", GREATER_THAN, 100),
                    Condition::new("monkeys.#", LESS_THAN_OR_EQUAL, 12),
                ]),
                Condition::not(Condition::and(vec![
                    Condition::new("coconuts", LESS_THAN, 10),
                    Condition::not(Condition::new("area", GREATER_THAN, 50)),
                ])),
                Condition::new("name", SAME_AS, "Julian"),
                Condition::new("name", MATCHES, "^King "),
            ]))
            .order_by("name")
            .limit(10)
            .offset(20),
    );

    assert_round_trip(
        "query test: orderby name",
        Query::new("test:").order_by("name"),
    );
    assert_round_trip("query test: limit 10", Query::new("test:").limit(10));
    assert_round_trip("query test: offset 10", Query::new("test:").offset(10));
    assert_round_trip(
        "query test: where banana matches ^ban",
        Query::new("test:").where_(Condition::new("banana", MATCHES, "^ban")),
    );
    assert_round_trip(
        "query test: where banana exists",
        Query::new("test:").where_(Condition::new("banana", EXISTS, ())),
    );
    assert_round_trip(
        "query test: where banana not exists",
        Query::new("test:").where_(Condition::not(Condition::new("banana", EXISTS, ()))),
    );

    // All operators.
    for (text, condition) in [
        (
            "query test: where banana == 1",
            Condition::new("banana", EQUALS, 1),
        ),
        (
            "query test: where banana > 1",
            Condition::new("banana", GREATER_THAN, 1),
        ),
        (
            "query test: where banana >= 1",
            Condition::new("banana", GREATER_THAN_OR_EQUAL, 1),
        ),
        (
            "query test: where banana < 1",
            Condition::new("banana", LESS_THAN, 1),
        ),
        (
            "query test: where banana <= 1",
            Condition::new("banana", LESS_THAN_OR_EQUAL, 1),
        ),
        (
            "query test: where banana f== 1.1",
            Condition::new("banana", FLOAT_EQUALS, 1.1),
        ),
        (
            "query test: where banana f> 1.1",
            Condition::new("banana", FLOAT_GREATER_THAN, 1.1),
        ),
        (
            "query test: where banana f>= 1.1",
            Condition::new("banana", FLOAT_GREATER_THAN_OR_EQUAL, 1.1),
        ),
        (
            "query test: where banana f< 1.1",
            Condition::new("banana", FLOAT_LESS_THAN, 1.1),
        ),
        (
            "query test: where banana f<= 1.1",
            Condition::new("banana", FLOAT_LESS_THAN_OR_EQUAL, 1.1),
        ),
        (
            "query test: where banana sameas banana",
            Condition::new("banana", SAME_AS, "banana"),
        ),
        (
            "query test: where banana contains banana",
            Condition::new("banana", CONTAINS, "banana"),
        ),
        (
            "query test: where banana startswith banana",
            Condition::new("banana", STARTS_WITH, "banana"),
        ),
        (
            "query test: where banana endswith banana",
            Condition::new("banana", ENDS_WITH, "banana"),
        ),
        (
            "query test: where banana in banana,coconut",
            Condition::new("banana", IN, vec!["banana", "coconut"]),
        ),
        (
            "query test: where banana matches banana",
            Condition::new("banana", MATCHES, "banana"),
        ),
        (
            "query test: where banana is true",
            Condition::new("banana", IS, true),
        ),
    ] {
        assert_round_trip(text, Query::new("test:").where_(condition));
    }
}

#[test]
fn parse_errors_match_go() {
    // Syntax errors.
    for (text, expected) in [
        ("query", "unexpected end at position 5"),
        ("query test: where", "unexpected end at position 17"),
        ("query test: where (", "unexpected end at position 19"),
        (
            "query test: where )",
            "unknown clause \")\" at position 19",
        ),
        ("query test: where not", "unexpected end at position 21"),
        ("query test: where banana", "unexpected end at position 24"),
        (
            "query test: where banana >",
            "unexpected end at position 26",
        ),
        (
            "query test: where banana nope",
            "unknown operator at position 26",
        ),
        (
            "query test: where banana exists or",
            "unexpected end at position 34",
        ),
        (
            "query test: where banana exists and",
            "unexpected end at position 35",
        ),
        (
            "query test: where banana exists and (",
            "unexpected end at position 37",
        ),
        (
            "query test: where banana exists and banana is true or",
            "you may not mix \"and\" and \"or\" (position: 52)",
        ),
        (
            "query test: where banana exists or banana is true and",
            "you may not mix \"and\" and \"or\" (position: 51)",
        ),
        ("", "unexpected end at position 0"),
        ("quer test:", "queries must start with \"query\""),
        (
            "query test: where \"foo bar\"",
            "unexpected end at position 27",
        ),
        (
            "query test: where foo == bar orderby name orderby name",
            "duplicate \"orderby\" clause found at position 43",
        ),
        (
            "query test: where foo == bar limit 1 limit 2",
            "duplicate \"limit\" clause found at position 38",
        ),
        (
            "query test: where foo == bar offset 1 offset 2",
            "duplicate \"offset\" clause found at position 39",
        ),
        (
            "query test: where foo == bar where baz == 1",
            "unknown operator at position 36",
        ),
        ("query test: where ()", "unexpected end at position 20"),
        (
            "query test: where (foo == 1)",
            "unexpected end at position 28",
        ),
        ("query test: where banana is", "unexpected end at position 27"),
        (
            "query test: where banana == bar limit -1",
            "could not parse integer (-1) at position 39",
        ),
    ] {
        assert_eq!(
            parse_query(text).unwrap_err().to_string(),
            expected,
            "{text:?}"
        );
    }

    // Value parsing errors.
    for (text, expected) in [
        (
            "query test: where banana == banana",
            "could not parse banana to int64: strconv.ParseInt: parsing \"banana\": invalid syntax (hint: use \"sameas\" to compare strings)",
        ),
        (
            "query test: where banana f== banana",
            "could not parse banana to float64: strconv.ParseFloat: parsing \"banana\": invalid syntax",
        ),
        (
            "query test: where banana in banana",
            "could not parse \"banana\" to []string",
        ),
        (
            "query test: where banana is great",
            "could not parse \"great\" to bool: strconv.ParseBool: parsing \"great\": invalid syntax",
        ),
        (
            "query test: where banana is 2",
            "could not parse \"2\" to bool: strconv.ParseBool: parsing \"2\": invalid syntax",
        ),
        (
            "query test: where banana f== 1e400",
            "could not parse 1e400 to float64: strconv.ParseFloat: parsing \"1e400\": value out of range",
        ),
        (
            "query test: where banana == 99999999999999999999",
            "could not parse 99999999999999999999 to int64: strconv.ParseInt: parsing \"99999999999999999999\": value out of range (hint: use \"sameas\" to compare strings)",
        ),
    ] {
        assert_eq!(
            parse_query(text).unwrap_err().to_string(),
            expected,
            "{text:?}"
        );
    }

    // Go and Rust regex engines report different detail messages; only the
    // failure itself is compatible.
    assert!(parse_query("query test: where banana matches [banana").is_err());
    assert!(parse_query("query test: where banana re (").is_err());
    // `in` with an empty element is accepted, like in Go.
    assert_eq!(
        parse_query("query test: where banana in ,").unwrap().print(),
        "query test: where banana in ,"
    );
}

// The gjson test document from `query_test.go`, made valid JSON.
const TEST_JSON: &str = r#"{"age":100, "name":{"here":"B\\\"R"},
  "noop":{"what is a wren?":"a bird"},
  "happy":true,"immortal":false,
  "items":[1,2,3,{"tags":[1,2,3],"points":[[1,2],[3,4]]},4,5,6,7],
  "arr":["1",2,"3",{"hello":"world"},"4",5],
  "vals":[1,2,3,{"sadf":"asdf"}],"name":{"first":"tom","last":null},
  "created":"2014-05-16T08:28:06.989Z",
  "loggy":{
  	"programmers": [
    	    {
    	        "firstName": "Brett",
    	        "lastName": "McLaughlin",
    	        "email": "aaaa",
  			"tag": "good"
    	    },
    	    {
    	        "firstName": "Jason",
    	        "lastName": "Hunter",
    	        "email": "bbbb",
  			"tag": "bad"
    	    },
    	    {
    	        "firstName": "Elliotte",
    	        "lastName": "Harold",
    	        "email": "cccc",
  			"tag": "good"
    	    },
  		{
  			"firstName": 1002.3,
  			"age": 101
  		}
    	]
  },
  "lastly":{"yay":"final"},
	"temperature": 120.413
}"#;

fn test_json_record() -> Record {
    let value: serde_json::Value = serde_json::from_str(TEST_JSON).unwrap();
    Record::from_typed(Key::parse("test:"), Meta::default(), &value).unwrap()
}

fn test_query(record: &Record, should_match: bool, condition: Condition) {
    let query = Query::new("test:").where_(condition).check().unwrap();
    assert_eq!(
        query.matches(record),
        should_match,
        "query: {}",
        query.print()
    );
}

#[test]
fn query_matching_matches_go() {
    let record = test_json_record();

    test_query(&record, true, Condition::new("age", EQUALS, 100));
    test_query(&record, true, Condition::new("age", GREATER_THAN, 99u8));
    test_query(&record, true, Condition::new("age", GREATER_THAN_OR_EQUAL, 99));
    test_query(&record, true, Condition::new("age", GREATER_THAN_OR_EQUAL, 100));
    test_query(&record, true, Condition::new("age", LESS_THAN, 101));
    test_query(&record, true, Condition::new("age", LESS_THAN_OR_EQUAL, "101"));
    test_query(&record, true, Condition::new("age", LESS_THAN_OR_EQUAL, 100));

    test_query(&record, true, Condition::new("temperature", FLOAT_EQUALS, 120.413));
    test_query(&record, true, Condition::new("temperature", FLOAT_GREATER_THAN, 120));
    test_query(&record, true, Condition::new("temperature", FLOAT_GREATER_THAN_OR_EQUAL, 120));
    test_query(&record, true, Condition::new("temperature", FLOAT_GREATER_THAN_OR_EQUAL, 120.413));
    test_query(&record, true, Condition::new("temperature", FLOAT_LESS_THAN, 121));
    test_query(&record, true, Condition::new("temperature", FLOAT_LESS_THAN_OR_EQUAL, "121"));
    test_query(&record, true, Condition::new("temperature", FLOAT_LESS_THAN_OR_EQUAL, "120.413"));

    test_query(&record, true, Condition::new("lastly.yay", SAME_AS, "final"));
    test_query(&record, true, Condition::new("lastly.yay", CONTAINS, "ina"));
    test_query(&record, true, Condition::new("lastly.yay", STARTS_WITH, "fin"));
    test_query(&record, true, Condition::new("lastly.yay", ENDS_WITH, "nal"));
    test_query(&record, true, Condition::new("lastly.yay", IN, "draft,final"));
    test_query(&record, true, Condition::new("lastly.yay", IN, "final,draft"));

    test_query(&record, true, Condition::new("happy", IS, true));
    test_query(&record, true, Condition::new("happy", IS, "true"));
    test_query(&record, true, Condition::new("happy", IS, "t"));
    test_query(&record, true, Condition::not(Condition::new("happy", IS, "0")));
    test_query(
        &record,
        true,
        Condition::and(vec![
            Condition::new("happy", IS, "1"),
            Condition::not(Condition::or(vec![
                Condition::new("happy", IS, false),
                Condition::new("happy", IS, "f"),
            ])),
        ]),
    );

    test_query(&record, true, Condition::new("happy", EXISTS, ()));
    assert!(!Query::new("test:")
        .where_(Condition::new("missing", EXISTS, ()))
        .check()
        .unwrap()
        .matches(&record));

    test_query(
        &record,
        true,
        Condition::new("created", MATCHES, "^2014-[0-9]{2}-[0-9]{2}T"),
    );

    // Like Go, `matches` only compares the key prefix, not the database name.
    let other = Query::new("test:other/").where_(Condition::new("age", EQUALS, 100));
    assert!(!other.check().unwrap().matches(&record));
}

#[test]
fn condition_value_types_match_go() {
    for condition in [
        Condition::new("banana", EQUALS, 1u32),
        Condition::new("banana", EQUALS, 1u16),
        Condition::new("banana", EQUALS, 1u8),
        Condition::new("banana", EQUALS, 1i64),
        Condition::new("banana", EQUALS, 1i32),
        Condition::new("banana", EQUALS, 1i16),
        Condition::new("banana", EQUALS, 1i8),
        Condition::new("banana", EQUALS, "1"),
        Condition::new("banana", FLOAT_EQUALS, 1u32),
        Condition::new("banana", FLOAT_EQUALS, 1i64),
        Condition::new("banana", FLOAT_EQUALS, 1.0f32),
        Condition::new("banana", FLOAT_EQUALS, 1.0f64),
        Condition::new("banana", FLOAT_EQUALS, "1.1"),
        Condition::new("banana", SAME_AS, "coconut"),
        Condition::new("banana", MATCHES, "coconut"),
        Condition::new("banana", IN, vec!["banana", "coconut"]),
        Condition::new("banana", IN, "banana,coconut"),
        Condition::new("banana", IS, true),
        Condition::new("banana", IS, "t"),
    ] {
        condition.check().unwrap_or_else(|err| panic!("{condition}: {err}"));
    }

    // Invalid value types.
    for condition in [
        Condition::new("banana", IS, 1),
        Condition::new("banana", FLOAT_EQUALS, true),
        Condition::new("banana", EQUALS, true),
        Condition::new("banana", SAME_AS, 1),
        Condition::new("banana", MATCHES, 1),
        Condition::new("banana", IN, 1),
    ] {
        assert!(condition.check().is_err(), "{condition} should fail");
    }

    // Conditions that carry their own error.
    for condition in [
        Condition::new("banana", 255, true),
        Condition::and(vec![Condition::new("banana", 255, true)]),
        Condition::or(vec![Condition::new("banana", 255, true)]),
        Condition::new("banana", 255, ()),
        Condition::new("banana", 254, ()),
    ] {
        assert!(condition.check().is_err(), "{condition} should fail");
    }
}

#[test]
fn query_key_prefix_and_lengths() {
    let q = parsed("query core:test/ where Count > 1 orderby Name limit 5 offset 2");
    assert_eq!(q.database_name(), "core");
    assert_eq!(q.database_key_prefix(), "test/");
    assert_eq!(q.prefix(), "core:test/");
    assert_eq!(q.order_by_key().as_deref(), Some("Name"));
    assert_eq!(q.limit_value(), 5);
    assert_eq!(q.offset_value(), 2);
    assert!(q.has_where_condition());
    assert!(q.matches_key("test/a/b"));
    assert!(!q.matches_key("other/a"));
    assert!(q.is_checked());

    // `limit 0` is indistinguishable from "no limit", like in Go.
    let q = parsed("query core: limit 0 offset 0");
    assert_eq!(q.print(), "query core:");
    assert_eq!(q.limit_value(), 0);
    assert_eq!(q.order_by_key(), None);

    // A prefix without a colon is the database name, the key prefix is empty.
    let q = parsed("query core");
    assert_eq!(q.database_name(), "core");
    assert_eq!(q.database_key_prefix(), "");
    assert_eq!(q.print(), "query core:");
}
