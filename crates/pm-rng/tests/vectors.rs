//! Deterministic compatibility vectors taken from the Go reference
//! implementations: `seehuhn/fortuna/generator_test.go` and
//! `aead/serpent/vectors_test.go`.

use std::io::Read as _;

use pm_rng::{bytes, number, read, CipherKind, Generator, Reader};

fn hex(s: &str) -> Vec<u8> {
    (0..s.len())
        .step_by(2)
        .map(|i| u8::from_str_radix(&s[i..i + 2], 16).unwrap())
        .collect()
}

#[test]
fn fortuna_golden_output() {
    // Mirrors fortuna `TestOutput` exactly:
    //   NewGenerator(aes.NewCipher); reset(); Reseed([1,2,3,4]).
    let mut gen = Generator::new(CipherKind::Aes);
    gen.reset();
    gen.reseed(&[1, 2, 3, 4]);

    let out = gen.pseudo_random_data(100);
    assert_eq!(
        out,
        hex("52fee98bfe5506dede957823ad4759e833b6fc8b99996f1e10077cb99f183244ec6b8512d9db2e86a99cd34aa31164ad1a46f6c139a4a7afe9dca07202c8d750cfda553aeb75b1df57c032fb3d418d643be417d73a6bf8f867397f1ff15be62100a44d2e")
    );

    // Continue for 1 MiB: the last 100 bytes exercise rekeying at
    // MAX_BLOCKS boundaries.
    let out = gen.pseudo_random_data((1 << 20) + 100);
    let tail = &out[1 << 20..];
    assert_eq!(
        tail,
        hex("7aa41a4366411ed9db710e56d692b9116b87b70712a27ece2e2636acf8c27654a292539c9860c00f17e0714c1508e229a1abc5b48aec7e896519dbe103bd10f2215b221b08abab739d6df8c6e312ccd32ab85c2aabdec675a286746d4dc3bb8b254ee03f").as_slice()
    );

    gen.reseed(&[5]);
    let out = gen.pseudo_random_data(100);
    assert_eq!(
        out,
        hex("d9a88da72e09dabc627c6d80f216bd78b47c0fc07495d388fd843c031dfa5f4285c3254ef2ffa0d1b96a44695391a548b3a735feb7fb80454e9cdb1a7cca2309aea72980b81902013f8ecda24544cffb650a1d218557bd24e53811648a314fefd2bd8d2e")
    );
}

#[test]
fn generator_seed_is_deterministic() {
    // Go `TestSeed`: reseeding with the same int64 reproduces output.
    for seed in [0i64, 1, 1 << 62] {
        for kind in [CipherKind::Aes, CipherKind::Serpent] {
            let mut a = Generator::new(kind);
            a.seed(seed);
            let first = a.pseudo_random_data(256);
            let mut b = Generator::new(kind);
            b.seed(seed);
            assert_eq!(first, b.pseudo_random_data(256), "{kind:?}/{seed}");
        }
    }
}

#[test]
fn serpent_block_vectors() {
    let cases = [
        (
            "80000000000000000000000000000000",
            "00000000000000000000000000000000",
            "264e5481eff42a4606abda06c0bfda3d",
        ),
        (
            "40000000000000000000000000000000",
            "00000000000000000000000000000000",
            "4a231b3bc727993407ac6ec8350e8524",
        ),
        (
            "800000000000000000000000000000000000000000000000",
            "00000000000000000000000000000000",
            "9e274ead9b737bb21efcfca548602689",
        ),
        (
            "100000000000000000000000000000000000000000000000",
            "00000000000000000000000000000000",
            "bec1e37824cf721e5d87f6cb4ebfb9be",
        ),
        (
            "0101010101010101010101010101010101010101010101010101010101010101",
            "01010101010101010101010101010101",
            "ec9723b15b2a6489f84c4524fffc2748",
        ),
        (
            "0202020202020202020202020202020202020202020202020202020202020202",
            "02020202020202020202020202020202",
            "1187f485538514476184e567da0421c7",
        ),
    ];
    for (key_hex, pt_hex, ct_hex) in cases {
        let key_bytes = hex(key_hex);
        let cipher = pm_rng::cipher::new_serpent(&key_bytes);
        let mut block: [u8; 16] = hex(pt_hex).try_into().unwrap();
        cipher.encrypt_block(&mut block);
        assert_eq!(block.to_vec(), hex(ct_hex), "key {key_hex}");
    }
}

#[test]
fn global_api_and_reader_work() {
    let n = 64;
    let mut via_read = vec![0u8; n];
    assert_eq!(read(&mut via_read).unwrap(), n);

    // The global generator advances between calls, so only the shape is
    // compared here (the deterministic vectors cover byte-for-byte checks).
    let mut via_reader = vec![0u8; n];
    Reader.read_exact(&mut via_reader).unwrap();
    assert!(via_reader.iter().any(|&b| b != 0));

    assert_eq!(bytes(n).unwrap().len(), n);
    assert!(number(100).unwrap() <= 100);
}
