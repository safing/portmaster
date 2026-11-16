//! Safe, printable body teasers, ported from Go `base/utils/safe.go`.
//!
//! `Notifier` uses these to include a bounded, escaped preview of an
//! unexpected response body in an error message.

/// Returns the first 16 bytes of `data` in the same safe form as Go's
/// `utils.SafeFirst16Bytes`.
///
/// Go implements this as the first line of `encoding/hex.Dump(data)` with the
/// `00000000  ` offset prefix removed. This is a faithful reimplementation of
/// that format: lowercase hex bytes separated by spaces (with an extra space
/// after the 8th byte), padded to 16 columns, followed by the ASCII rendering
/// where non-printable bytes become `.`. Only the first 16 bytes are shown;
/// any remaining bytes are ignored. An empty input yields `<empty>`.
pub fn safe_first_16_bytes(data: &[u8]) -> String {
    if data.is_empty() {
        return "<empty>".to_string();
    }

    // Go's hex.Dump only puts 16 bytes on a line, and the helper keeps only
    // the first line, so anything past byte 16 is irrelevant.
    let shown = data.len().min(16);
    let mut out = String::with_capacity(68);

    for i in 0..16 {
        match data.get(i) {
            Some(&byte) => {
                out.push(HEX_DIGITS[(byte >> 4) as usize] as char);
                out.push(HEX_DIGITS[(byte & 0x0f) as usize] as char);
            }
            None => out.push_str("  "),
        }

        // Space after every byte slot.
        out.push(' ');

        if i == 7 {
            // hex.Dump inserts an extra space after the 8th byte.
            out.push(' ');
        } else if i == 15 {
            // At the end of the line, another space precedes the ASCII bar.
            out.push(' ');
            out.push('|');
        }
    }

    for &byte in data.iter().take(shown) {
        out.push(if (0x20..=0x7e).contains(&byte) {
            byte as char
        } else {
            '.'
        });
    }
    out.push('|');

    out
}

/// Returns the first 16 bytes of `s` (as UTF-8) in safe form.
///
/// Equivalent to Go's `utils.SafeFirst16Chars`, which simply converts the
/// string to bytes and delegates to `SafeFirst16Bytes`.
pub fn safe_first_16_chars(s: &str) -> String {
    safe_first_16_bytes(s.as_bytes())
}

const HEX_DIGITS: &[u8; 16] = b"0123456789abcdef";

#[cfg(test)]
mod tests {
    use super::{safe_first_16_bytes, safe_first_16_chars};

    // Reference values were produced by running the Go implementation
    // (encoding/hex.Dump + TrimPrefix) against the same inputs.
    #[test]
    fn empty_input() {
        assert_eq!(safe_first_16_bytes(b""), "<empty>");
        assert_eq!(safe_first_16_chars(""), "<empty>");
    }

    #[test]
    fn matches_go_dump_for_lengths_one_to_sixteen() {
        let cases = [
            (1, "41                                                |A|"),
            (2, "41 42                                             |AB|"),
            (3, "41 42 43                                          |ABC|"),
            (4, "41 42 43 44                                       |ABCD|"),
            (5, "41 42 43 44 45                                    |ABCDE|"),
            (6, "41 42 43 44 45 46                                 |ABCDEF|"),
            (7, "41 42 43 44 45 46 47                              |ABCDEFG|"),
            (8, "41 42 43 44 45 46 47 48                           |ABCDEFGH|"),
            (9, "41 42 43 44 45 46 47 48  49                       |ABCDEFGHI|"),
            (10, "41 42 43 44 45 46 47 48  49 4a                    |ABCDEFGHIJ|"),
            (11, "41 42 43 44 45 46 47 48  49 4a 4b                 |ABCDEFGHIJK|"),
            (12, "41 42 43 44 45 46 47 48  49 4a 4b 4c              |ABCDEFGHIJKL|"),
            (13, "41 42 43 44 45 46 47 48  49 4a 4b 4c 4d           |ABCDEFGHIJKLM|"),
            (14, "41 42 43 44 45 46 47 48  49 4a 4b 4c 4d 4e        |ABCDEFGHIJKLMN|"),
            (15, "41 42 43 44 45 46 47 48  49 4a 4b 4c 4d 4e 4f     |ABCDEFGHIJKLMNO|"),
            (16, "41 42 43 44 45 46 47 48  49 4a 4b 4c 4d 4e 4f 50  |ABCDEFGHIJKLMNOP|"),
        ];

        for (len, expected) in cases {
            let data: Vec<u8> = (0..len).map(|i| b'A' + (i as u8 % 26)).collect();
            assert_eq!(safe_first_16_bytes(&data), expected, "length {len}");
        }
    }

    #[test]
    fn only_first_sixteen_bytes_are_shown() {
        let data: Vec<u8> = (0..26).map(|i| b'A' + i as u8).collect();
        let expected = "41 42 43 44 45 46 47 48  49 4a 4b 4c 4d 4e 4f 50  |ABCDEFGHIJKLMNOP|";
        assert_eq!(safe_first_16_bytes(&data), expected);
    }

    #[test]
    fn escapes_non_printable_bytes_and_keeps_utf8_bytes() {
        assert_eq!(
            safe_first_16_bytes(b"hello world\n"),
            "68 65 6c 6c 6f 20 77 6f  72 6c 64 0a              |hello world.|"
        );
        assert_eq!(
            safe_first_16_bytes(&[0x00, 0x01, 0xff]),
            "00 01 ff                                          |...|"
        );
        // SafeFirst16Chars operates on UTF-8 bytes, just like Go's []byte(s).
        assert_eq!(
            safe_first_16_chars("hello world\n"),
            "68 65 6c 6c 6f 20 77 6f  72 6c 64 0a              |hello world.|"
        );
    }
}
