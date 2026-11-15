use pm_dsd::list::ListReader;
use pm_dsd::{dump, format, load, varint};

#[test]
fn varint_truncated_or_overflow() {
    assert!(varint::unpack8(&[]).is_err());
    assert!(varint::unpack8(&[0x80]).is_err());
    assert!(varint::unpack8(&[0x80, 0x02]).is_err());
    assert!(varint::unpack16(&[]).is_err());
    assert!(varint::unpack16(&[0x80]).is_err());
    assert!(varint::unpack16(&[0x80, 0x80, 0x04]).is_err());
    assert!(varint::unpack32(&[0xff; 4]).is_err());
    assert!(varint::unpack32(&[0xff, 0xff, 0xff, 0xff, 0x1f]).is_err());
    assert!(varint::unpack64(&[0xff]).is_err());
    assert!(varint::unpack64(&[0xff; 9]).is_err());
    let mut ten = vec![0xff; 9];
    ten.push(0x02);
    assert!(varint::unpack64(&ten).is_err());
    assert!(varint::unpack64(&[0xff; 11]).is_err());
}

#[test]
fn next_block_truncated() {
    assert!(varint::next_block(&[]).is_err());
    assert!(varint::next_block(&[5, 1, 2]).is_err());
    let mut huge = varint::pack64(u64::MAX);
    huge.push(1);
    assert!(varint::next_block(&huge).is_err());
    assert_eq!(varint::next_block(&[0]).unwrap(), (&[][..], 1));
}

#[test]
fn load_corrupt() {
    type R = Result<(serde_json::Value, u8), pm_dsd::Error>;
    assert!(load::<serde_json::Value>(&[]).is_err());
    assert!(load::<serde_json::Value>(&[format::JSON]).is_err());
    assert!(load::<serde_json::Value>(&[format::JSON, b'{']).is_err());
    assert!(load::<serde_json::Value>(&[format::CBOR, 0xa6]).is_err());
    assert!(load::<serde_json::Value>(&[format::MSG_PACK, 0x86]).is_err());
    assert!(load::<serde_json::Value>(&[format::GZIP, 1, 2, 3]).is_err());
    assert!(load::<serde_json::Value>(&[format::GZIP]).is_err());
    assert!(matches!(
        load::<serde_json::Value>(&[format::RAW, 1]),
        Err(pm_dsd::Error::IsRaw)
    ));
    assert!(matches!(
        load::<serde_json::Value>(&[format::GEN_CODE, 1]),
        Err(pm_dsd::Error::Unsupported(_))
    ));
    assert!(matches!(
        load::<serde_json::Value>(&[format::YAML, 1]),
        Err(pm_dsd::Error::Unsupported(_))
    ));
    assert!(load::<serde_json::Value>(&[0x55, 1]).is_err());
    assert!(load::<serde_json::Value>(&[0x80]).is_err());
    // Trailing garbage after a gzip member.
    let mut gz = dump(&serde_json::json!({"a": 1}), format::GZIP).unwrap();
    gz.extend_from_slice(&[1, 2, 3, 4]);
    assert!(load::<serde_json::Value>(&gz).is_err());
    // Truncated gzip stream.
    let full = dump(&serde_json::json!({"a": "x".repeat(200)}), format::GZIP).unwrap();
    for n in 0..full.len() {
        let r: R = load(&full[..n]);
        assert!(r.is_err(), "prefix {n} must fail");
    }
}

#[test]
fn truncated_json_prefixes_never_panic() {
    let full = dump(&serde_json::json!({"a": [1, 2, 3], "b": "c"}), format::JSON).unwrap();
    for n in 0..full.len() {
        assert!(load::<serde_json::Value>(&full[..n]).is_err());
    }
}

#[test]
fn list_reader() {
    let mut blob = vec![format::LIST, format::JSON];
    blob.extend(varint::prepend_length(b"{\"a\":1}"));
    blob.extend(varint::prepend_length(b"{}"));
    let r = ListReader::new(&blob).unwrap();
    assert_eq!(r.format(), format::JSON);
    let items: Vec<_> = r.map(|e| e.unwrap().0.into_owned()).collect();
    assert_eq!(items, vec![b"{\"a\":1}".to_vec(), b"{}".to_vec()]);

    // Compressed variant.
    let mut raw = varint::prepend_length(b"one");
    raw.extend(varint::prepend_length(b"two"));
    let mut enc = flate2::write::GzEncoder::new(
        vec![format::LIST, format::GZIP, format::CBOR],
        flate2::Compression::default(),
    );
    std::io::Write::write_all(&mut enc, &raw).unwrap();
    let comp = enc.finish().unwrap();
    let r = ListReader::new(&comp).unwrap();
    assert!(r.is_compressed());
    assert_eq!(r.format(), format::CBOR);
    let items: Vec<_> = r.map(|e| e.unwrap().0.into_owned()).collect();
    assert_eq!(items, vec![b"one".to_vec(), b"two".to_vec()]);
}

#[test]
fn list_reader_corrupt() {
    assert!(ListReader::new(&[]).is_err());
    assert!(ListReader::new(&[format::LIST]).is_err());
    assert!(ListReader::new(&[b'X', format::JSON]).is_err());
    assert!(ListReader::new(&[format::LIST, format::GZIP]).is_err());
    assert!(ListReader::new(&[format::LIST, format::GZIP, format::JSON, 1, 2]).is_err());
    // Header only: no entries.
    assert_eq!(
        ListReader::new(&[format::LIST, format::JSON])
            .unwrap()
            .count(),
        0
    );
    // Truncated blob, then iteration stops.
    let mut it = ListReader::new(&[format::LIST, format::JSON, 5, 1, 2]).unwrap();
    assert!(it.next().unwrap().is_err());
    assert!(it.next().is_none());
    // Truncated length varint.
    let mut it = ListReader::new(&[format::LIST, format::JSON, 0x80]).unwrap();
    assert!(it.next().unwrap().is_err());
}
