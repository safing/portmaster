//! Tests for creating jess JSON signatures against Go-generated fixtures.

use pm_jess::{
    add_json_signature, verify_json_signature, DirTrustStore, Envelope, TrustStore,
};

fn fixtures() -> std::path::PathBuf {
    std::path::Path::new(env!("CARGO_MANIFEST_DIR"))
        .join("../fixtures/m0/jess-ts")
}

fn index() -> Vec<u8> {
    std::fs::read(
        std::path::Path::new(env!("CARGO_MANIFEST_DIR")).join("../fixtures/m0/jess-index.json"),
    )
    .unwrap()
}

fn sign(envelope: &str, data: &[u8]) -> Vec<u8> {
    let ts = DirTrustStore::new(fixtures()).unwrap();
    let env = ts.get_envelope(envelope).unwrap();
    add_json_signature(data, &env, &ts).unwrap()
}

fn trust() -> TrustStore {
    DirTrustStore::new(fixtures())
        .unwrap()
        .verification_trust_store()
        .unwrap()
}

#[test]
fn all_signing_suites_round_trip() {
    let ts = trust();
    let index = index();
    for envelope in [
        "test-envelope",
        "env-signfile-v1",
        "env-sign-v2",
        "env-signfile-v2",
    ] {
        let signed = sign(envelope, &index);
        verify_json_signature(&signed, &ts)
            .unwrap_or_else(|e| panic!("{envelope}: signature does not verify: {e}"));
        // The signed document is still an index with the same content.
        let split = pm_jess::json_split(&signed).unwrap();
        assert_eq!(split.content, pm_jess::json_split(&index).unwrap().content);
        assert_eq!(split.signatures.len(), 1, "{envelope}");
    }
}

#[test]
fn multiple_senders_are_all_signed() {
    let signed = sign("env-multi", &index());
    let split = pm_jess::json_split(&signed).unwrap();
    // One letter, containing one seal per sender.
    assert_eq!(split.signatures.len(), 1);
    let raw = base64_decode(&split.signatures[0]);
    let letter = pm_jess::Letter::from_dsd(&raw).unwrap();
    assert_eq!(letter.signatures.len(), 2);
    assert_eq!(letter.signatures[0].id, "test-signer");
    assert_eq!(letter.signatures[1].id, "test-signer-2");
    verify_json_signature(&signed, &trust()).unwrap();
}

fn base64_decode(text: &str) -> Vec<u8> {
    use base64::Engine;
    base64::engine::general_purpose::URL_SAFE_NO_PAD
        .decode(text)
        .unwrap()
}

#[test]
fn text_format_signer_is_supported() {
    let signed = sign("env-text", &index());
    verify_json_signature(&signed, &trust()).unwrap();
}

#[test]
fn existing_signatures_and_checksums_are_preserved() {
    let first = sign("test-envelope", &index());
    let with_checksum = pm_jess::json_split(&first).unwrap();
    assert!(with_checksum.checksums.is_empty());

    let second = sign("env-multi", &first);
    let split = pm_jess::json_split(&second).unwrap();
    assert_eq!(split.signatures.len(), 2);
    // Sorted, like Go's slices.Sort + Compact.
    assert!(split.signatures.windows(2).all(|w| w[0] <= w[1]));
    verify_json_signature(&second, &trust()).unwrap();
}

#[test]
fn existing_checksums_are_sorted_and_deduplicated() {
    let input = br#"{"Artifacts":[],"Name":"test-index","Published":"2026-11-15T14:28:06Z","Version":"v1.2.3","_jess-checksum":["b","a","a"]}"#;
    let signed = sign("test-envelope", input);
    let split = pm_jess::json_split(&signed).unwrap();
    assert_eq!(split.checksums, ["a", "b"]);
    assert_eq!(split.signatures.len(), 1);
    verify_json_signature(&signed, &trust()).unwrap();
}

#[test]
fn signature_over_tampered_content_is_rejected() {
    let signed = String::from_utf8(sign("test-envelope", &index())).unwrap();
    let tampered = signed.replace("v1.2.3", "v1.2.4");
    assert!(verify_json_signature(tampered.as_bytes(), &trust()).is_err());
}

#[test]
fn envelope_text_format_is_supported() {
    let envelope = "envelope:sign_v1:test-envelope:67SYgyqBC2wBfjWcifxEPS9gJp7hpFKFp3ETvc9T8ac2juFsRyiEUApdWBB3QRH7UyzM2pMMZEzemRFoiGpTHqHQ4BYr2AuiBkpPt1YKRS3tqTmb6UAgQWQmsNkQB7Ng27nuFWDhbaTC6mny2PobKPwURH3fvm2Kvq1dw8LRmx4dAkMKet7yPfsaeB";
    let parsed = Envelope::from_text(envelope).unwrap();
    assert_eq!(parsed.suite_id, "sign_v1");
    assert_eq!(parsed.name, "test-envelope");
    assert_eq!(parsed.senders[0].id, "test-signer");
}

#[test]
fn unsupported_suite_and_missing_key_are_rejected() {
    let ts = DirTrustStore::new(fixtures()).unwrap();
    let mut envelope = ts.get_envelope("test-envelope").unwrap();
    envelope.suite_id = "v1".into();
    assert!(add_json_signature(&index(), &envelope, &ts).is_err());

    let envelope = ts.get_envelope("test-envelope").unwrap();
    let missing = DirTrustStore::new(
        std::path::Path::new(env!("CARGO_MANIFEST_DIR")).join("../fixtures/m0"),
    )
    .unwrap();
    assert!(add_json_signature(&index(), &envelope, &missing).is_err());
}
