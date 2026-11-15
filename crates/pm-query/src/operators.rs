//! Operator codes, spellings and their primary print names.
//!
//! Ported from `base/database/query/condition.go` and `operators.go`. The codes
//! are stored in records and compared against `ERROR_PRESENT`, so they must not
//! be reordered.

/// Integer comparison operators.
pub const EQUALS: u8 = 0;
pub const GREATER_THAN: u8 = 1;
pub const GREATER_THAN_OR_EQUAL: u8 = 2;
pub const LESS_THAN: u8 = 3;
pub const LESS_THAN_OR_EQUAL: u8 = 4;
/// Float comparison operators.
pub const FLOAT_EQUALS: u8 = 5;
pub const FLOAT_GREATER_THAN: u8 = 6;
pub const FLOAT_GREATER_THAN_OR_EQUAL: u8 = 7;
pub const FLOAT_LESS_THAN: u8 = 8;
pub const FLOAT_LESS_THAN_OR_EQUAL: u8 = 9;
/// String operators.
pub const SAME_AS: u8 = 10;
pub const CONTAINS: u8 = 11;
pub const STARTS_WITH: u8 = 12;
pub const ENDS_WITH: u8 = 13;
/// String slice operator.
pub const IN: u8 = 14;
/// Regex operator.
pub const MATCHES: u8 = 15;
/// Boolean operator.
pub const IS: u8 = 16;
/// Any operator.
pub const EXISTS: u8 = 17;

/// Internal marker for a condition that failed to build.
pub const ERROR_PRESENT: u8 = 255;

/// All operator spellings accepted by the parser.
const NAMES: &[(&str, u8)] = &[
    ("==", EQUALS),
    (">", GREATER_THAN),
    (">=", GREATER_THAN_OR_EQUAL),
    ("<", LESS_THAN),
    ("<=", LESS_THAN_OR_EQUAL),
    ("f==", FLOAT_EQUALS),
    ("f>", FLOAT_GREATER_THAN),
    ("f>=", FLOAT_GREATER_THAN_OR_EQUAL),
    ("f<", FLOAT_LESS_THAN),
    ("f<=", FLOAT_LESS_THAN_OR_EQUAL),
    ("sameas", SAME_AS),
    ("s==", SAME_AS),
    ("contains", CONTAINS),
    ("co", CONTAINS),
    ("startswith", STARTS_WITH),
    ("sw", STARTS_WITH),
    ("endswith", ENDS_WITH),
    ("ew", ENDS_WITH),
    ("in", IN),
    ("matches", MATCHES),
    ("re", MATCHES),
    ("is", IS),
    ("exists", EXISTS),
    ("ex", EXISTS),
];

/// Returns the operator code for a spelling, like Go `operatorNames`.
pub fn from_name(name: &[u8]) -> Option<u8> {
    NAMES
        .iter()
        .find(|(n, _)| n.as_bytes() == name)
        .map(|(_, op)| *op)
}

/// Returns the spelling used when printing, like Go `getOpName`.
///
/// Go picks the longest spelling for every operator, which is unambiguous.
pub fn name(operator: u8) -> &'static str {
    match operator {
        EQUALS => "==",
        GREATER_THAN => ">",
        GREATER_THAN_OR_EQUAL => ">=",
        LESS_THAN => "<",
        LESS_THAN_OR_EQUAL => "<=",
        FLOAT_EQUALS => "f==",
        FLOAT_GREATER_THAN => "f>",
        FLOAT_GREATER_THAN_OR_EQUAL => "f>=",
        FLOAT_LESS_THAN => "f<",
        FLOAT_LESS_THAN_OR_EQUAL => "f<=",
        SAME_AS => "sameas",
        CONTAINS => "contains",
        STARTS_WITH => "startswith",
        ENDS_WITH => "endswith",
        IN => "in",
        MATCHES => "matches",
        IS => "is",
        EXISTS => "exists",
        _ => "[unknown]",
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn every_spelling_round_trips() {
        for (spelling, op) in NAMES {
            assert_eq!(from_name(spelling.as_bytes()), Some(*op), "{spelling}");
        }
        assert_eq!(from_name(b"nope"), None);
    }

    #[test]
    fn primary_names_match_go() {
        assert_eq!(name(ERROR_PRESENT), "[unknown]");
        assert_eq!(name(254), "[unknown]");
        assert_eq!(name(SAME_AS), "sameas");
        assert_eq!(name(MATCHES), "matches");
    }
}
