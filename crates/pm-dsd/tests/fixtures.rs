use std::collections::BTreeMap;

use pm_dsd::{dump, format, gojson, load, varint};
use serde::{Deserialize, Serialize};
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
fn varint_cases() {
    for c in fixture("varint.json") {
        let bits = c["bits"].as_u64().unwrap();
        let packed = hx(&c["packed"]);
        if bits == 0 {
            assert_eq!(varint::prepend_length(&hx(&c["input"])), packed);
            let (block, used) = varint::next_block(&packed).unwrap();
            assert_eq!((block, used), (&b"hello"[..], packed.len()));
            continue;
        }
        let num = |v: &Value| -> u64 {
            v.as_u64()
                .unwrap_or_else(|| v.as_str().unwrap().parse().unwrap())
        };
        let n = num(&c["value"]);
        match bits {
            8 => {
                assert_eq!(varint::pack8(n as u8), packed);
                assert_eq!(varint::unpack8(&packed).unwrap().0 as u64, n);
            }
            16 => {
                assert_eq!(varint::pack16(n as u16), packed);
                assert_eq!(varint::unpack16(&packed).unwrap(), (n as u16, packed.len()));
            }
            32 => {
                assert_eq!(varint::pack32(n as u32), packed);
                assert_eq!(varint::unpack32(&packed).unwrap(), (n as u32, packed.len()));
            }
            64 => {
                assert_eq!(varint::pack64(n), packed);
                assert_eq!(varint::unpack64(&packed).unwrap(), (n, packed.len()));
            }
            _ => panic!("bad bits"),
        }
    }
}

#[test]
fn gojson_cases() {
    for c in fixture("gojson.json") {
        let expected = c["json"].as_str().unwrap();
        match c["kind"].as_str().unwrap() {
            "string" => {
                let s = String::from_utf8_lossy(&hx(&c["input"])).into_owned();
                assert_eq!(
                    String::from_utf8(gojson::to_vec(&s).unwrap()).unwrap(),
                    expected
                );
            }
            kind => {
                let v: Value = serde_json::from_str(expected).unwrap();
                // Go sorts map keys; re-sort the parsed value the same way.
                let sorted = sort(&v);
                let out = if kind == "map" {
                    gojson::to_vec(&sorted).unwrap()
                } else {
                    gojson::to_vec_indent(&sorted, "", "  ").unwrap()
                };
                assert_eq!(String::from_utf8(out).unwrap(), expected, "{kind}");
            }
        }
    }
}

fn sort(v: &Value) -> Value {
    match v {
        Value::Object(m) => {
            let b: BTreeMap<_, _> = m.iter().map(|(k, v)| (k.clone(), sort(v))).collect();
            serde_json::to_value(b).unwrap()
        }
        Value::Array(a) => Value::Array(a.iter().map(sort).collect()),
        o => o.clone(),
    }
}

#[test]
fn gojson_extras() {
    let s = |v: &dyn erased::Ser| String::from_utf8(v.to()).unwrap();
    assert_eq!(s(&"\u{8}\u{c}\u{7f}"), "\"\\b\\f\u{7f}\"");
    assert_eq!(s(&1e21f64), "1e+21");
    assert_eq!(s(&1e20f64), "100000000000000000000");
    assert_eq!(s(&1e-7f64), "1e-7");
    assert_eq!(s(&1.5e-10f64), "1.5e-10");
    assert_eq!(s(&0.000001f64), "0.000001");
    assert_eq!(s(&0.1f32), "0.1");
    assert_eq!(s(&0.0f64), "0");
    let nested: Vec<Vec<u8>> = vec![vec![], vec![1]];
    assert_eq!(
        String::from_utf8(gojson::to_vec_indent(&nested, ">", "\t").unwrap()).unwrap(),
        "[\n>\t[],\n>\t[\n>\t\t1\n>\t]\n>]"
    );
}

mod erased {
    pub trait Ser {
        fn to(&self) -> Vec<u8>;
    }
    impl<T: serde::Serialize> Ser for T {
        fn to(&self) -> Vec<u8> {
            pm_dsd::gojson::to_vec(self).unwrap()
        }
    }
}

#[derive(Debug, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "PascalCase")]
struct Sample {
    name: String,
    count: i64,
    ratio: f64,
    tags: Vec<String>,
    nested: BTreeMap<String, String>,
    flag: bool,
}

fn sample() -> Sample {
    Sample {
        name: "Ünïcode <&>".into(),
        count: -42,
        ratio: 0.25,
        tags: vec!["a".into(), "b|c".into()],
        nested: [
            ("z".to_string(), "1".to_string()),
            ("a".to_string(), "2".to_string()),
        ]
        .into(),
        flag: true,
    }
}

#[test]
fn dsd_cases() {
    for c in fixture("dsd.json") {
        let f = c["format"].as_u64().unwrap() as u8;
        let go = hx(&c["dump"]);
        let (loaded, got): (Sample, u8) = load(&go).unwrap();
        assert_eq!(loaded, sample(), "load format {f}");
        assert_eq!(got, if f == format::GZIP { format::JSON } else { f });

        let ours = dump(&sample(), f).unwrap();
        // Our own output must round-trip regardless of byte equality.
        assert_eq!(load::<Sample>(&ours).unwrap().0, sample());
        if f == format::JSON {
            assert_eq!(ours, go);
        }
        eprintln!("format {f}: byte-equal with Go = {}", ours == go);
    }
}
