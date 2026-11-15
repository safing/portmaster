use pm_record::{Key, Meta, Record};
use serde_json::Value;

fn fixture(name: &str) -> Vec<Value> {
    let path = concat!(env!("CARGO_MANIFEST_DIR"), "/../fixtures/m0/");
    let text = std::fs::read_to_string(format!("{path}{name}")).unwrap();
    let v: Value = serde_json::from_str(&text).unwrap();
    v["cases"].as_array().unwrap().clone()
}

fn hx(v: &Value) -> Vec<u8> {
    hex::decode(v.as_str().unwrap()).unwrap()
}

#[test]
fn meta_gencode_cases() {
    for c in fixture("meta_gencode.json") {
        let name = c["name"].as_str().unwrap();
        let meta = Meta {
            created: c["created"].as_i64().unwrap(),
            modified: c["modified"].as_i64().unwrap(),
            expires: c["expires"].as_i64().unwrap(),
            deleted: c["deleted"].as_i64().unwrap(),
            secret: c["secret"].as_bool().unwrap(),
            cronjewel: c["crownjewel"].as_bool().unwrap(),
        };
        let gc = hx(&c["gencode"]);
        assert_eq!(gc.len(), 34, "{name}");
        assert_eq!(meta.gencode_marshal(), gc, "{name}");
        assert_eq!(Meta::gencode_unmarshal(&gc).unwrap(), meta, "{name}");
        let dsd = hx(&c["dsd"]);
        assert_eq!(dsd[0], b'G');
        assert_eq!(&dsd[1..], &gc[..], "{name}");
        let json = pm_dsd::gojson::to_vec(&meta).unwrap();
        assert_eq!(
            String::from_utf8(json).unwrap(),
            c["json"].as_str().unwrap(),
            "{name}"
        );
    }
}

#[test]
fn meta_gencode_short_input_errors() {
    for n in 0..34 {
        assert!(Meta::gencode_unmarshal(&vec![0u8; n]).is_err());
    }
}

#[test]
fn wrapper_cases() {
    for c in fixture("wrapper.json") {
        let key = c["key"].as_str().unwrap();
        let raw = hx(&c["marshal_record"]);
        let rec = Record::from_raw(
            c["db"].as_str().unwrap(),
            c["db_key"].as_str().unwrap(),
            &raw,
        )
        .unwrap();
        assert_eq!(rec.key.to_string(), key);
        assert_eq!(
            rec.format,
            c["parsed_format"].as_u64().unwrap() as u8,
            "{key}"
        );
        assert_eq!(rec.data, hx(&c["parsed_data"]), "{key}");
        let m = &c["meta"];
        assert_eq!(rec.meta.created, m["Created"].as_i64().unwrap());
        assert_eq!(rec.meta.modified, m["Modified"].as_i64().unwrap());
        assert_eq!(rec.meta.expires, m["Expires"].as_i64().unwrap());
        assert_eq!(rec.meta.deleted, m["Deleted"].as_i64().unwrap());
        assert_eq!(rec.meta.is_deleted(), c["deleted"].as_bool().unwrap());
        if key.contains("secret") {
            assert!(rec.meta.secret && rec.meta.cronjewel);
        }
        assert_eq!(rec.marshal_record(), raw, "{key}");
    }
}

#[test]
fn api_record_cases() {
    let wrappers = fixture("wrapper.json");
    for c in fixture("api_record.json") {
        let key = c["key"].as_str().unwrap();
        let w = wrappers.iter().find(|w| w["key"] == key).unwrap();
        let rec = Record::from_raw(
            w["db"].as_str().unwrap(),
            w["db_key"].as_str().unwrap(),
            &hx(&w["marshal_record"]),
        )
        .unwrap();
        if c.get("error").is_some() {
            assert!(rec.to_json_with_meta(true).is_err(), "{key}");
            assert!(rec.to_json_with_meta(false).is_err(), "{key}");
            continue;
        }
        let without = rec.to_json_with_meta(false).unwrap();
        assert_eq!(
            String::from_utf8(without).unwrap(),
            c["without_dsd_id"].as_str().unwrap(),
            "{key}"
        );
        assert_eq!(
            rec.to_json_with_meta(true).unwrap(),
            hx(&c["with_dsd_id"]),
            "{key}"
        );
    }
}

#[test]
fn truncated_wrappers_never_panic() {
    for c in fixture("wrapper.json") {
        let raw = hx(&c["marshal_record"]);
        for n in 0..raw.len() {
            let _ = Record::from_raw("core", "x", &raw[..n]);
        }
    }
    assert!(Record::from_raw("core", "x", &[2, 0]).is_err());
}

#[test]
fn key_parse() {
    let k = Key::parse("core:a:b");
    assert_eq!((k.db.as_str(), k.key.as_str()), ("core", "a:b"));
    assert_eq!(k.to_string(), "core:a:b");
    // Go ParseKey returns the whole string as database name when there is no colon.
    let k = Key::parse("nocolon");
    assert_eq!((k.db.as_str(), k.key.as_str()), ("nocolon", ""));
}

#[test]
fn typed_roundtrip_and_key_escaping() {
    #[derive(serde::Serialize, serde::Deserialize, PartialEq, Debug)]
    struct T {
        a: String,
        b: i64,
    }
    let t = T {
        a: "x<y".into(),
        b: 5,
    };
    let rec = Record::from_typed(Key::parse("core:a<b&c"), Meta::default(), &t).unwrap();
    assert_eq!(rec.decode::<T>().unwrap(), t);
    // sjson writes plain ASCII keys without HTML escaping.
    let out = String::from_utf8(rec.to_json_with_meta(false).unwrap()).unwrap();
    assert!(out.ends_with(r#""Key":"core:a<b&c"}}"#), "{out}");
    let rec = Record::from_typed(Key::parse("core:q\"é"), Meta::default(), &t).unwrap();
    let out = String::from_utf8(rec.to_json_with_meta(false).unwrap()).unwrap();
    assert!(out.ends_with(r#""Key":"core:q\"é"}}"#), "{out}");
    let mut bad = rec.clone();
    bad.data = b"[1]".to_vec();
    assert!(bad.to_json_with_meta(false).is_err());
}
