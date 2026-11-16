//! Block ciphers used by the Fortuna generator.
//!
//! The Go `base/rng` package selects its cipher through `newCipher`, which
//! supports `"aes"` (default) and `"serpent"`. Both produce a 128-bit block
//! and accept the 256-bit (32 byte) generator key.

use aes::cipher::generic_array::GenericArray;
use aes::cipher::{BlockEncrypt, KeyInit};

/// The ciphers selectable by the Go `rngCipher` global.
#[derive(Copy, Clone, Debug, PartialEq, Eq)]
pub enum CipherKind {
    Aes,
    Serpent,
}

impl CipherKind {
    /// Parse the Go cipher names (`"aes"`, `"serpent"`).
    pub fn parse(name: &str) -> Option<CipherKind> {
        match name {
            "aes" => Some(CipherKind::Aes),
            "serpent" => Some(CipherKind::Serpent),
            _ => None,
        }
    }

    pub fn as_str(self) -> &'static str {
        match self {
            CipherKind::Aes => "aes",
            CipherKind::Serpent => "serpent",
        }
    }

    /// Allocate a cipher for the 32-byte Fortuna generator key.
    pub fn new_cipher(self, key: &[u8; 32]) -> Box<dyn BlockCipher> {
        match self {
            CipherKind::Aes => Box::new(AesCipher::new(key)),
            CipherKind::Serpent => Box::new(SerpentCipher::new(key)),
        }
    }
}

/// Build a Serpent cipher from a 128/192/256-bit key, reproducing Go's
/// `serpent.NewCipher` key-size handling (Fortuna always passes 32 bytes).
pub fn new_serpent(key: &[u8]) -> Box<dyn BlockCipher> {
    Box::new(SerpentCipher::new(key))
}

/// Minimal 128-bit block-cipher interface needed by Fortuna (encryption only).
pub trait BlockCipher: Send + Sync {
    fn encrypt_block(&self, block: &mut [u8; 16]);
    fn kind(&self) -> CipherKind;
}

// ---------------------------------------------------------------------------
// AES-256
// ---------------------------------------------------------------------------

struct AesCipher(aes::Aes256);

impl AesCipher {
    fn new(key: &[u8; 32]) -> AesCipher {
        AesCipher(aes::Aes256::new_from_slice(key).expect("AES-256 key must be 32 bytes"))
    }
}

impl BlockCipher for AesCipher {
    fn encrypt_block(&self, block: &mut [u8; 16]) {
        let mut buf = GenericArray::clone_from_slice(block);
        self.0.encrypt_block(&mut buf);
        block.copy_from_slice(buf.as_slice());
    }

    fn kind(&self) -> CipherKind {
        CipherKind::Aes
    }
}

// ---------------------------------------------------------------------------
// Serpent (port of github.com/aead/serpent, reference implementation)
// ---------------------------------------------------------------------------

const PHI: u32 = 0x9e37_79b9;

struct SerpentCipher {
    sk: [u32; 132],
}

impl SerpentCipher {
    fn new(key: &[u8]) -> SerpentCipher {
        let mut sk = [0u32; 132];
        let mut k = [0u32; 16];
        for (j, chunk) in key.chunks_exact(4).enumerate() {
            k[j] = u32::from_le_bytes([chunk[0], chunk[1], chunk[2], chunk[3]]);
        }
        // For 32-byte keys all eight words are filled; keep the Go padding
        // rule for completeness (`if j < 8 { k[j] = 1 }`).
        let words = key.len() / 4;
        if words < 8 {
            k[words] = 1;
        }

        for i in 8..16 {
            let x = k[i - 8] ^ k[i - 5] ^ k[i - 3] ^ k[i - 1] ^ PHI ^ (i as u32 - 8);
            k[i] = x.rotate_left(11);
            sk[i - 8] = k[i];
        }
        for i in 8..132 {
            let x = sk[i - 8] ^ sk[i - 5] ^ sk[i - 3] ^ sk[i - 1] ^ PHI ^ (i as u32);
            sk[i] = x.rotate_left(11);
        }

        // The Go code applies the S-boxes in this fixed order to each group
        // of four subkeys, for 32 groups, then sb3 to the final group.
        type Sbox = fn(&mut S);
        const ORDER: [Sbox; 8] = [sb3, sb2, sb1, sb0, sb7, sb6, sb5, sb4];
        for g in 0..32 {
            let base = g * 4;
            let mut state = (sk[base], sk[base + 1], sk[base + 2], sk[base + 3]);
            ORDER[g % 8](&mut state);
            sk[base..base + 4].copy_from_slice(&[state.0, state.1, state.2, state.3]);
        }
        let mut state = (sk[128], sk[129], sk[130], sk[131]);
        sb3(&mut state);
        sk[128..132].copy_from_slice(&[state.0, state.1, state.2, state.3]);

        SerpentCipher { sk }
    }
}

type S = (u32, u32, u32, u32);

impl BlockCipher for SerpentCipher {
    fn encrypt_block(&self, block: &mut [u8; 16]) {
        let mut r = (
            u32::from_le_bytes([block[0], block[1], block[2], block[3]]),
            u32::from_le_bytes([block[4], block[5], block[6], block[7]]),
            u32::from_le_bytes([block[8], block[9], block[10], block[11]]),
            u32::from_le_bytes([block[12], block[13], block[14], block[15]]),
        );

        const SBOXES: [fn(&mut S); 8] = [sb0, sb1, sb2, sb3, sb4, sb5, sb6, sb7];
        for round in 0..32 {
            let base = round * 4;
            r.0 ^= self.sk[base];
            r.1 ^= self.sk[base + 1];
            r.2 ^= self.sk[base + 2];
            r.3 ^= self.sk[base + 3];
            SBOXES[round % 8](&mut r);
            if round < 31 {
                linear(&mut r);
            }
        }
        r.0 ^= self.sk[128];
        r.1 ^= self.sk[129];
        r.2 ^= self.sk[130];
        r.3 ^= self.sk[131];

        block[0..4].copy_from_slice(&r.0.to_le_bytes());
        block[4..8].copy_from_slice(&r.1.to_le_bytes());
        block[8..12].copy_from_slice(&r.2.to_le_bytes());
        block[12..16].copy_from_slice(&r.3.to_le_bytes());
    }

    fn kind(&self) -> CipherKind {
        CipherKind::Serpent
    }
}

/// Serpent's linear transformation.
fn linear(v: &mut S) {
    let t0 = v.0.rotate_left(13);
    let t2 = v.2.rotate_left(3);
    let t1 = v.1 ^ t0 ^ t2;
    let t3 = v.3 ^ t2 ^ (t0 << 3);
    v.1 = t1.rotate_left(1);
    v.3 = t3.rotate_left(7);
    let t0 = t0 ^ v.1 ^ v.3;
    let t2 = t2 ^ v.3 ^ (v.1 << 7);
    v.0 = t0.rotate_left(5);
    v.2 = t2.rotate_left(22);
}

// S-boxes: bit-sliced Gladman/Simpson variants, translated line-for-line from
// github.com/aead/serpent (sbox_ref.go).
fn sb0(r: &mut S) {
    let t0 = r.0 ^ r.3;
    let t1 = r.2 ^ t0;
    let t2 = r.1 ^ t1;
    r.3 = (r.0 & r.3) ^ t2;
    let t3 = r.0 ^ (r.1 & t0);
    r.2 = t2 ^ (r.2 | t3);
    let t4 = r.3 & (t1 ^ t3);
    r.1 = !t1 ^ t4;
    r.0 = t4 ^ !t3;
}

fn sb1(r: &mut S) {
    let t0 = r.1 ^ !r.0;
    let t1 = r.2 ^ (r.0 | t0);
    r.2 = r.3 ^ t1;
    let t2 = r.1 ^ (r.3 | t0);
    let t3 = t0 ^ r.2;
    r.3 = t3 ^ (t1 & t2);
    let t4 = t1 ^ t2;
    r.1 = r.3 ^ t4;
    r.0 = t1 ^ (t3 & t4);
}

fn sb2(r: &mut S) {
    let v0 = r.0;
    let v3 = r.3;
    let t0 = !v0;
    let t1 = r.1 ^ v3;
    let t2 = r.2 & t0;
    r.0 = t1 ^ t2;
    let t3 = r.2 ^ t0;
    let t4 = r.2 ^ r.0;
    let t5 = r.1 & t4;
    r.3 = t3 ^ t5;
    r.2 = v0 ^ ((v3 | t5) & (r.0 | t3));
    r.1 = (t1 ^ r.3) ^ (r.2 ^ (v3 | t0));
}

fn sb3(r: &mut S) {
    let v1 = r.1;
    let v3 = r.3;
    let t0 = r.0 ^ r.1;
    let t1 = r.0 & r.2;
    let t2 = r.0 | r.3;
    let t3 = r.2 ^ r.3;
    let t4 = t0 & t2;
    let t5 = t1 | t4;
    r.2 = t3 ^ t5;
    let t6 = r.1 ^ t2;
    let t7 = t5 ^ t6;
    let t8 = t3 & t7;
    r.0 = t0 ^ t8;
    let t9 = r.2 & r.0;
    r.1 = t7 ^ t9;
    r.3 = (v1 | v3) ^ (t3 ^ t9);
}

fn sb4(r: &mut S) {
    let v0 = r.0;
    let t0 = v0 ^ r.3;
    let t1 = r.3 & t0;
    let t2 = r.2 ^ t1;
    let t3 = r.1 | t2;
    r.3 = t0 ^ t3;
    let t4 = !r.1;
    let t5 = t0 | t4;
    r.0 = t2 ^ t5;
    let t6 = v0 & r.0;
    let t7 = t0 ^ t4;
    let t8 = t3 & t7;
    r.2 = t6 ^ t8;
    r.1 = (v0 ^ t2) ^ (t7 & r.2);
}

fn sb5(r: &mut S) {
    let v1 = r.1;
    let t0 = !r.0;
    let t1 = r.0 ^ v1;
    let t2 = r.0 ^ r.3;
    let t3 = r.2 ^ t0;
    let t4 = t1 | t2;
    r.0 = t3 ^ t4;
    let t5 = r.3 & r.0;
    let t6 = t1 ^ r.0;
    r.1 = t5 ^ t6;
    let t7 = t0 | r.0;
    let t8 = t1 | t5;
    let t9 = t2 ^ t7;
    r.2 = t8 ^ t9;
    r.3 = (v1 ^ t5) ^ (r.1 & t9);
}

fn sb6(r: &mut S) {
    let t0 = !r.0;
    let t1 = r.0 ^ r.3;
    let t2 = r.1 ^ t1;
    let t3 = t0 | t1;
    let t4 = r.2 ^ t3;
    r.1 ^= t4;
    let t5 = t1 | r.1;
    let t6 = r.3 ^ t5;
    let t7 = t4 & t6;
    r.2 = t2 ^ t7;
    let t8 = t4 ^ t6;
    r.0 = r.2 ^ t8;
    r.3 = !t4 ^ (t2 & t8);
}

fn sb7(r: &mut S) {
    let t0 = r.1 ^ r.2;
    let t1 = r.2 & t0;
    let t2 = r.3 ^ t1;
    let t3 = r.0 ^ t2;
    let t4 = r.3 | t0;
    let t5 = t3 & t4;
    r.1 ^= t5;
    let t6 = t2 | r.1;
    let t7 = r.0 & t3;
    r.3 = t0 ^ t7;
    let t8 = t3 ^ t6;
    let t9 = r.3 & t8;
    r.2 = t2 ^ t9;
    r.0 = !t8 ^ (r.3 & r.2);
}

