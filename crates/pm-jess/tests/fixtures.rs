//! Compatibility tests against fixtures produced by the real Go jess library.

use pm_jess::{
    intel_trust_store, json_split, verify_json_checksum, verify_json_signature, TrustStore,
    BINARY_SIGNING_KEYS, INTEL_SIGNING_KEYS,
};
use serde_json::Value;

fn fixture() -> Value {
    let path = concat!(env!("CARGO_MANIFEST_DIR"), "/../fixtures/m0/jess.json");
    serde_json::from_str(&std::fs::read_to_string(path).unwrap()).unwrap()
}

fn trust_set(fx: &Value, name: &str) -> TrustStore {
    let keys = &fx["signing_keys"];
    let list: Vec<&str> = match name {
        "a" => vec!["a"],
        "ab" => vec!["a", "b"],
        "b" => vec!["b"],
        "file" => vec!["file"],
        "v2" => vec!["v2"],
        "empty" => vec![],
        other => panic!("unknown trust set {other}"),
    };
    let strings: Vec<&str> = list.iter().map(|k| keys[*k].as_str().unwrap()).collect();
    TrustStore::from_signet_strings(&strings).unwrap()
}

fn strings(v: &Value) -> Vec<String> {
    v.as_array()
        .map(|a| a.iter().map(|s| s.as_str().unwrap().to_string()).collect())
        .unwrap_or_default()
}

#[test]
fn signature_cases_match_go() {
    let fx = fixture();
    let mut failures = Vec::new();
    for case in fx["cases"].as_array().unwrap() {
        let name = case["name"].as_str().unwrap();
        let input = case["input"].as_str().unwrap().as_bytes();
        let ts = trust_set(&fx, case["trust_set"].as_str().unwrap());

        let want_sig = case["sig_error"].as_str().unwrap();
        let got_sig = verify_json_signature(input, &ts)
            .err()
            .map(|e| e.to_string())
            .unwrap_or_default();
        if want_sig != got_sig {
            failures.push(format!(
                "{name}: signature: go {want_sig:?}, rust {got_sig:?}"
            ));
        }

        let want_sum = case["sum_error"].as_str().unwrap();
        let got_sum = verify_json_checksum(input)
            .err()
            .map(|e| e.to_string())
            .unwrap_or_default();
        if want_sum != got_sum {
            failures.push(format!(
                "{name}: checksum: go {want_sum:?}, rust {got_sum:?}"
            ));
        }

        let split = json_split(input);
        if case["split_error"].as_str().unwrap().is_empty() {
            let split = split.unwrap_or_else(|| panic!("{name}: split failed"));
            if split.content != case["content"].as_str().unwrap().as_bytes() {
                failures.push(format!(
                    "{name}: content differs\n go: {}\n rust: {}",
                    case["content"].as_str().unwrap(),
                    String::from_utf8_lossy(&split.content)
                ));
            }
            assert_eq!(split.checksums, strings(&case["checksums"]), "{name}");
            assert_eq!(split.signatures, strings(&case["signatures"]), "{name}");
        } else {
            assert!(split.is_none(), "{name}: split should fail");
        }
    }
    assert!(failures.is_empty(), "{}", failures.join("\n"));
}

#[test]
fn valid_cases_succeed_and_tampered_fail() {
    let fx = fixture();
    let by_name = |n: &str| {
        fx["cases"]
            .as_array()
            .unwrap()
            .iter()
            .find(|c| c["name"] == n)
            .unwrap()
            .clone()
    };
    for (name, ok) in [
        ("valid", true),
        ("two-signatures", true),
        ("signfile-suite", true),
        ("sign-v2-suite", true),
        ("whitespace-independent", true),
        ("escaped-signature-key", true),
        ("escaped-signature-key-tampered", false),
        ("tampered-content", false),
        ("tampered-signature", false),
        ("unsigned", false),
        ("unknown-signer", false),
        ("two-signatures-one-untrusted", false),
    ] {
        let case = by_name(name);
        let ts = trust_set(&fx, case["trust_set"].as_str().unwrap());
        let res = verify_json_signature(case["input"].as_str().unwrap().as_bytes(), &ts);
        assert_eq!(res.is_ok(), ok, "{name}: {res:?}");
    }
    let unsigned = by_name("unsigned");
    let ts = trust_set(&fx, "a");
    assert_eq!(
        verify_json_signature(unsigned["input"].as_str().unwrap().as_bytes(), &ts),
        Err(pm_jess::Error::SignatureMissing)
    );
}

#[test]
fn pretty_matches_go() {
    let fx = fixture();
    for case in fx["pretty_cases"].as_array().unwrap() {
        let input = case["input"].as_str().unwrap();
        if !case["valid"].as_bool().unwrap() {
            continue;
        }
        let got = pm_jess::pretty::pretty_options(
            input.as_bytes(),
            &pm_jess::pretty::Options::canonical(),
        );
        assert_eq!(
            String::from_utf8_lossy(&got),
            case["output"].as_str().unwrap(),
            "input: {input}"
        );
    }
}

#[test]
fn validity_matches_gjson() {
    let fx = fixture();
    for case in fx["valid_cases"].as_array().unwrap() {
        let input = case["input"].as_str().unwrap();
        assert_eq!(
            pm_jess::verify_json_checksum(input.as_bytes()).err()
                != Some(pm_jess::Error::InvalidJson),
            case["valid"].as_bool().unwrap(),
            "input: {input:?}"
        );
    }
}

#[test]
fn go_signing_keys_parse() {
    assert_eq!(intel_trust_store().len(), INTEL_SIGNING_KEYS.len());
    assert_eq!(
        pm_jess::binary_trust_store().len(),
        BINARY_SIGNING_KEYS.len()
    );
    assert_eq!(INTEL_SIGNING_KEYS.len(), 2);
    assert_eq!(BINARY_SIGNING_KEYS.len(), 2);
}

#[test]
fn bad_signet_strings_are_rejected() {
    for bad in [
        "",
        "sender:abc",
        "recipient:",
        "recipient:0OIl",
        "recipient:abc",
    ] {
        assert!(TrustStore::from_signet_strings(&[bad]).is_err(), "{bad}");
    }
}

#[test]
fn index_verifier_adapter() {
    let fx = fixture();
    let case = fx["cases"]
        .as_array()
        .unwrap()
        .iter()
        .find(|c| c["name"] == "valid")
        .unwrap();
    let ts = trust_set(&fx, "a");
    let verify = pm_jess::index_verifier(&ts);
    assert!(verify(case["input"].as_str().unwrap().as_bytes()).is_ok());
    assert!(verify(b"{}").is_err());
}

#[test]
fn corrupt_input_never_panics() {
    let fx = fixture();
    let ts = trust_set(&fx, "ab");
    for name in ["valid", "two-signatures", "checksum-and-signature"] {
        let case = fx["cases"]
            .as_array()
            .unwrap()
            .iter()
            .find(|c| c["name"] == name)
            .unwrap();
        let input = case["input"].as_str().unwrap().as_bytes().to_vec();
        for cut in 0..input.len() {
            let _ = verify_json_signature(&input[..cut], &ts);
            let _ = verify_json_checksum(&input[..cut]);
        }
        for pos in (0..input.len()).step_by(7) {
            let mut m = input.clone();
            m[pos] ^= 0x55;
            let _ = verify_json_signature(&m, &ts);
            let _ = verify_json_checksum(&m);
        }
    }
}

#[test]
fn deeply_nested_json_is_rejected_not_overflowed() {
    let ts = TrustStore::new();
    let deep = format!("{}{}", "[".repeat(100_000), "]".repeat(100_000));
    assert!(verify_json_signature(deep.as_bytes(), &ts).is_err());
    let ok = format!("{}{}", "[".repeat(200), "]".repeat(200));
    assert_eq!(
        verify_json_signature(ok.as_bytes(), &ts),
        Err(pm_jess::Error::SignatureMissing)
    );
}

/// Verifies a real downloaded intel index. Set `PM_JESS_REAL_INDEX` to use
/// another location.
#[test]
#[ignore = "needs a real intel index on this machine"]
fn real_intel_index() {
    let path = std::env::var("PM_JESS_REAL_INDEX")
        .unwrap_or_else(|_| "/var/lib/portmaster/intel/index.json".to_string());
    let data = std::fs::read(&path).unwrap_or_else(|e| panic!("read {path}: {e}"));
    verify_json_signature(&data, &intel_trust_store()).unwrap();
}
