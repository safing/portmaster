//! Cross-checks the Rust implementation against output produced by the real
//! Go Fortuna/Serpent/sha256d libraries via `tools/rust-fixtures -only-rng`.

use pm_rng::cipher::new_serpent;
use pm_rng::sha256d::sha256d;
use pm_rng::{CipherKind, Generator};
use serde_json::Value;

const FIXTURE: &str = concat!(
    env!("CARGO_MANIFEST_DIR"),
    "/../fixtures/m0/rng.json"
);

fn hex(s: &str) -> Vec<u8> {
    (0..s.len())
        .step_by(2)
        .map(|i| u8::from_str_radix(&s[i..i + 2], 16).unwrap())
        .collect()
}

#[test]
fn matches_go_fixture() {
    let path = std::path::Path::new(FIXTURE);
    let data = std::fs::read_to_string(path)
        .unwrap_or_else(|e| panic!("missing Go fixture {}: {e}", path.display()));
    let fixture: Value = serde_json::from_str(&data).expect("valid rng.json");
    let cases = fixture["cases"].as_array().expect("cases array");

    let mut checked = 0;
    for case in cases {
        match case["kind"].as_str().expect("kind") {
            "sha256d" => {
                let input = hex(case["in_hex"].as_str().unwrap());
                let expected = case["out_hex"].as_str().unwrap();
                assert_eq!(to_hex(&sha256d(&input)), expected);
                checked += 1;
            }
            "serpent" => {
                let key = hex(case["key_hex"].as_str().unwrap());
                let mut block: [u8; 16] = hex(case["pt_hex"].as_str().unwrap())
                    .try_into()
                    .unwrap();
                new_serpent(&key).encrypt_block(&mut block);
                assert_eq!(block.to_vec(), hex(case["ct_hex"].as_str().unwrap()));
                checked += 1;
            }
            "fortuna" => {
                let kind = match case["cipher"].as_str().unwrap() {
                    "aes" => CipherKind::Aes,
                    "serpent" => CipherKind::Serpent,
                    other => panic!("unknown cipher {other}"),
                };
                let seed = case["seed"].as_i64().unwrap();
                let mut gen = Generator::new(kind);
                gen.seed(seed);
                for r in case["reseeds"].as_array().unwrap() {
                    gen.reseed(&hex(r.as_str().unwrap()));
                }
                let mut output = Vec::new();
                for n in case["lengths"].as_array().unwrap() {
                    output.extend(gen.pseudo_random_data(n.as_u64().unwrap() as usize));
                }
                assert_eq!(
                    to_hex(&output),
                    case["out_hex"].as_str().unwrap(),
                    "fortuna/{kind:?}/seed={seed}"
                );
                checked += 1;
            }
            other => panic!("unknown fixture kind {other}"),
        }
    }
    assert_eq!(checked, cases.len());
    assert!(checked >= 12, "expected all Go fixture cases to be checked");
}

fn to_hex(bytes: &[u8]) -> String {
    bytes.iter().map(|b| format!("{b:02x}")).collect()
}
