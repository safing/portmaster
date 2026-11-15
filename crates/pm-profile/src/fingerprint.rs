//! Process fingerprints: matching, scoring and profile ID derivation.

use std::collections::BTreeMap;

use regex::Regex;
use serde::{Deserialize, Serialize};

use pm_dsd::varint;

/// Fingerprint type: process tag.
pub const FINGERPRINT_TYPE_TAG: &str = "tag";
/// Fingerprint type: command line.
pub const FINGERPRINT_TYPE_CMDLINE: &str = "cmdline";
/// Fingerprint type: environment variable.
pub const FINGERPRINT_TYPE_ENV: &str = "env";
/// Fingerprint type: path (matches path and matching path).
pub const FINGERPRINT_TYPE_PATH: &str = "path";

/// Fingerprint operation: exact match.
pub const FINGERPRINT_OPERATION_EQUALS: &str = "equals";
/// Fingerprint operation: prefix match.
pub const FINGERPRINT_OPERATION_PREFIX: &str = "prefix";
/// Fingerprint operation: regular expression match.
pub const FINGERPRINT_OPERATION_REGEX: &str = "regex";

const TAG_MATCH_BASE_SCORE: i32 = 50_000;
const CMDLINE_MATCH_BASE_SCORE: i32 = 40_000;
const ENV_MATCH_BASE_SCORE: i32 = 30_000;
const MATCHING_PATH_MATCH_BASE_SCORE: i32 = 20_000;
const PATH_MATCH_BASE_SCORE: i32 = 10_000;

const FINGERPRINT_EQUALS_BASE_SCORE: i32 = 3_000;
const FINGERPRINT_PREFIX_BASE_SCORE: i32 = 2_000;
const FINGERPRINT_REGEX_BASE_SCORE: i32 = 1_000;

const MAX_MATCH_STRENGTH: i32 = 499;

/// A way of matching a process.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct Fingerprint {
    #[serde(rename = "Type")]
    pub type_: String,
    #[serde(rename = "Key")]
    pub key: String,
    #[serde(rename = "Operation")]
    pub operation: String,
    #[serde(rename = "Value")]
    pub value: String,
    #[serde(rename = "MergedFrom")]
    pub merged_from: String,
}

impl Fingerprint {
    pub fn new(type_: &str, key: &str, operation: &str, value: &str) -> Fingerprint {
        Fingerprint {
            type_: type_.to_string(),
            key: key.to_string(),
            operation: operation.to_string(),
            value: value.to_string(),
            merged_from: String::new(),
        }
    }

    pub fn with_merged_from(mut self, merged_from: &str) -> Fingerprint {
        self.merged_from = merged_from.to_string();
        self
    }

    /// Returns whether the optional fingerprint key matches.
    pub fn matches_key(&self, key: &str) -> bool {
        key == self.key
    }
}

/// A simple key/value tag used in process metadata.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct Tag {
    #[serde(rename = "Key")]
    pub key: String,
    #[serde(rename = "Value")]
    pub value: String,
}

/// Data of a process that fingerprints are matched against.
pub trait MatchingData {
    fn tags(&self) -> &[Tag];
    fn env(&self) -> &BTreeMap<String, String>;
    fn path(&self) -> &str;
    fn matching_path(&self) -> &str;
    fn cmdline(&self) -> &str;
}

/// Returns whether the key is in the tags.
pub fn key_in_tags(tags: &[Tag], key: &str) -> bool {
    tags.iter().any(|tag| tag.key == key)
}

/// Returns whether the key/value pair is in the tags.
pub fn key_and_value_in_tags(tags: &[Tag], key: &str, value: &str) -> bool {
    tags.iter().any(|tag| tag.key == key && tag.value == value)
}

enum Matcher {
    Equals,
    Prefix,
    Regex(Regex),
}

pub(crate) struct MatchingFingerprint {
    key: String,
    matcher: Matcher,
    value: String,
}

impl MatchingFingerprint {
    fn match_value(&self, value: &str) -> i32 {
        let score = match &self.matcher {
            Matcher::Equals => {
                if value == self.value {
                    FINGERPRINT_EQUALS_BASE_SCORE
                } else {
                    0
                }
            }
            Matcher::Prefix => {
                if value.starts_with(&self.value) {
                    FINGERPRINT_PREFIX_BASE_SCORE
                } else {
                    0
                }
            }
            Matcher::Regex(regex) => {
                let mut best = 0;
                for found in regex.find_iter(value) {
                    let length = found.end() - found.start();
                    if length > best {
                        best = length;
                    }
                }
                if best > 0 {
                    FINGERPRINT_REGEX_BASE_SCORE
                } else {
                    0
                }
            }
        };
        if score == 0 {
            return 0;
        }
        score + check_match_strength(self.match_strength(value))
    }

    fn match_strength(&self, value: &str) -> i32 {
        match &self.matcher {
            Matcher::Equals => self.value.len() as i32,
            Matcher::Prefix => {
                if value.starts_with(&self.value) {
                    self.value.len() as i32
                } else {
                    0
                }
            }
            Matcher::Regex(regex) => regex
                .find_iter(value)
                .map(|found| (found.end() - found.start()) as i32)
                .max()
                .unwrap_or(0),
        }
    }
}

/// Parsed fingerprints for fast matching.
#[derive(Default)]
pub struct ParsedFingerprints {
    tag_prints: Vec<MatchingFingerprint>,
    env_prints: Vec<MatchingFingerprint>,
    path_prints: Vec<MatchingFingerprint>,
    cmdline_prints: Vec<MatchingFingerprint>,
}

impl ParsedFingerprints {
    pub fn is_empty(&self) -> bool {
        self.tag_prints.is_empty()
            && self.env_prints.is_empty()
            && self.path_prints.is_empty()
            && self.cmdline_prints.is_empty()
    }
}

/// Parses fingerprints, returning the first error but keeping valid entries,
/// like Go `ParseFingerprints`.
pub fn parse_fingerprints(
    raw: &[Fingerprint],
    deprecated_linked_path: &str,
) -> (ParsedFingerprints, Option<String>) {
    let mut parsed = ParsedFingerprints::default();
    let mut first_error: Option<String> = None;
    let mut error = |message: String| {
        if first_error.is_none() {
            first_error = Some(message);
        }
    };

    // Deprecated linked path becomes an exact path fingerprint.
    if raw.is_empty() && !deprecated_linked_path.is_empty() {
        parsed.path_prints.push(MatchingFingerprint {
            key: String::new(),
            matcher: Matcher::Equals,
            value: deprecated_linked_path.to_string(),
        });
    }

    for entry in raw {
        match entry.type_.as_str() {
            FINGERPRINT_TYPE_TAG | FINGERPRINT_TYPE_ENV => {
                if entry.key.is_empty() {
                    error(format!("{} fingerprint is missing key", entry.type_));
                    continue;
                }
            }
            FINGERPRINT_TYPE_PATH | FINGERPRINT_TYPE_CMDLINE => {}
            _ => {
                error(format!("unknown fingerprint type: {:?}", entry.type_));
                continue;
            }
        }

        let matcher = match entry.operation.as_str() {
            FINGERPRINT_OPERATION_EQUALS => Matcher::Equals,
            FINGERPRINT_OPERATION_PREFIX => Matcher::Prefix,
            FINGERPRINT_OPERATION_REGEX => match Regex::new(&entry.value) {
                Ok(regex) => Matcher::Regex(regex),
                Err(_) => {
                    error(format!(
                        "failed to compile regex fingerprint: {}",
                        entry.value
                    ));
                    continue;
                }
            },
            _ => {
                error(format!(
                    "unknown fingerprint operation: {:?}",
                    entry.operation
                ));
                continue;
            }
        };

        let matching = MatchingFingerprint {
            key: entry.key.clone(),
            matcher,
            value: entry.value.clone(),
        };
        match entry.type_.as_str() {
            FINGERPRINT_TYPE_TAG => parsed.tag_prints.push(matching),
            FINGERPRINT_TYPE_ENV => parsed.env_prints.push(matching),
            FINGERPRINT_TYPE_PATH => parsed.path_prints.push(matching),
            FINGERPRINT_TYPE_CMDLINE => parsed.cmdline_prints.push(matching),
            _ => unreachable!("type was validated above"),
        }
    }

    (parsed, first_error)
}

/// Returns the highest matching score of the fingerprints and matching data.
pub fn match_fingerprints(prints: &ParsedFingerprints, md: &dyn MatchingData) -> i32 {
    let mut highest_score = 0;

    // Tags.
    let tags = md.tags();
    if !tags.is_empty() {
        for tag_print in &prints.tag_prints {
            for tag in tags {
                if !tag_print.matches_key_str(&tag.key) {
                    continue;
                }
                let score = tag_print.match_value(&tag.value);
                if score > highest_score {
                    highest_score = score;
                }
            }
        }
        if highest_score > 0 {
            return TAG_MATCH_BASE_SCORE + highest_score;
        }
    }

    // Command line.
    let cmdline = md.cmdline();
    if !cmdline.is_empty() {
        for cmdline_print in &prints.cmdline_prints {
            let score = cmdline_print.match_value(cmdline);
            if score > highest_score {
                highest_score = score;
            }
        }
        if highest_score > 0 {
            return CMDLINE_MATCH_BASE_SCORE + highest_score;
        }
    }

    // Environment.
    for env_print in &prints.env_prints {
        for (key, value) in md.env() {
            if !env_print.matches_key_str(key) {
                continue;
            }
            let score = env_print.match_value(value);
            if score > highest_score {
                highest_score = score;
            }
        }
    }
    if highest_score > 0 {
        return ENV_MATCH_BASE_SCORE + highest_score;
    }

    // Matching path.
    let matching_path = md.matching_path();
    if !matching_path.is_empty() {
        for path_print in &prints.path_prints {
            let score = path_print.match_value(matching_path);
            if score > highest_score {
                highest_score = score;
            }
        }
        if highest_score > 0 {
            return MATCHING_PATH_MATCH_BASE_SCORE + highest_score;
        }
    }

    // Path.
    let path = md.path();
    if !path.is_empty() {
        for path_print in &prints.path_prints {
            let score = path_print.match_value(path);
            if score > highest_score {
                highest_score = score;
            }
        }
        if highest_score > 0 {
            return PATH_MATCH_BASE_SCORE + highest_score;
        }
    }

    0
}

impl MatchingFingerprint {
    fn matches_key_str(&self, key: &str) -> bool {
        key == self.key
    }
}

fn check_match_strength(value: i32) -> i32 {
    value.clamp(-MAX_MATCH_STRENGTH, MAX_MATCH_STRENGTH)
}

const DERIVE_FP_KEY_ID_FOR_ITEM_START: u64 = 1;
const DERIVE_FP_KEY_ID_FOR_TYPE: u64 = 2;
const DERIVE_FP_KEY_ID_FOR_KEY: u64 = 3;
const DERIVE_FP_KEY_ID_FOR_OPERATION: u64 = 4;
const DERIVE_FP_KEY_ID_FOR_VALUE: u64 = 5;

/// Derives a profile ID from the given fingerprints, like Go `DeriveProfileID`.
///
/// Go sorts the fingerprints in place but hashes the un-compacted slice, so
/// duplicates are part of the derived ID. This is kept for compatibility.
pub fn derive_profile_id(fingerprints: &mut [Fingerprint]) -> String {
    // Go sorts in place and then compacts in place: `slices.CompactFunc` zeroes
    // the elements beyond the new length, and the caller's slice keeps its
    // length. The zeroed fingerprints are therefore part of the derived ID.
    sort_fingerprints(fingerprints);
    compact_with_zero_tail(fingerprints);

    let mut data = Vec::new();
    data.extend(varint::pack64(fingerprints.len() as u64));
    for fingerprint in fingerprints.iter() {
        data.extend(varint::pack64(DERIVE_FP_KEY_ID_FOR_ITEM_START));
        for (id, value) in [
            (DERIVE_FP_KEY_ID_FOR_TYPE, &fingerprint.type_),
            (DERIVE_FP_KEY_ID_FOR_KEY, &fingerprint.key),
            (DERIVE_FP_KEY_ID_FOR_OPERATION, &fingerprint.operation),
            (DERIVE_FP_KEY_ID_FOR_VALUE, &fingerprint.value),
        ] {
            if !value.is_empty() {
                data.extend(varint::pack64(id));
                data.extend(varint::prepend_length(value.as_bytes()));
            }
        }
    }

    // lhash SHA3-256 digest: algorithm ID 17 and the digest as a block.
    use sha3::{Digest, Sha3_256};
    let digest = Sha3_256::digest(&data);
    let mut labeled = varint::pack64(17);
    labeled.extend(varint::prepend_length(&digest));
    bs58::encode(labeled).into_string()
}

/// Compacts the slice in place and fills the tail with zero fingerprints, like
/// Go's `slices.CompactFunc`.
fn compact_with_zero_tail(fingerprints: &mut [Fingerprint]) {
    let length = fingerprints.len();
    let mut unique = 0;
    for index in 0..length {
        if unique == 0 || fingerprints[index] != fingerprints[unique - 1] {
            fingerprints[unique] = fingerprints[index].clone();
            unique += 1;
        }
    }
    for entry in fingerprints.iter_mut().take(length).skip(unique) {
        *entry = Fingerprint::default();
    }
}

/// Sorts fingerprints in place, like the sorting part of Go
/// `sortAndCompactFingerprints`.
pub fn sort_fingerprints(fingerprints: &mut [Fingerprint]) {
    fingerprints.sort_by(|a, b| {
        (&a.type_, &a.key, &a.operation, &a.value, &a.merged_from).cmp(&(
            &b.type_,
            &b.key,
            &b.operation,
            &b.value,
            &b.merged_from,
        ))
    });
}

/// Sorts and de-duplicates fingerprints in place, like Go
/// `sortAndCompactFingerprints`.
pub fn sort_and_compact_fingerprints(fingerprints: &mut Vec<Fingerprint>) {
    sort_fingerprints(fingerprints);
    fingerprints.dedup_by(|a, b| {
        a.type_ == b.type_
            && a.key == b.key
            && a.operation == b.operation
            && a.value == b.value
            && a.merged_from == b.merged_from
    });
}
