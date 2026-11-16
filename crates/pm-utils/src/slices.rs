//! Slice helpers ported from Go `base/utils/slices.go`.
//!
//! Go slices are `[]string` / `[]byte`; the Rust API uses `&[S]` with
//! `S: AsRef<str>` so both `&[String]` and `&[&str]` work. `index_of_string`
//! returns `Option<usize>` in place of Go's `-1` sentinel.

/// Returns the index of `s` in `a`, or `None` when it is not present.
///
/// Equivalent to Go's `IndexOfString`, which returns `-1`.
pub fn index_of_string<S: AsRef<str>>(a: &[S], s: &str) -> Option<usize> {
    a.iter().position(|entry| entry.as_ref() == s)
}

/// Returns whether `s` is in `a`.
///
/// Equivalent to Go's `StringInSlice`.
pub fn string_in_slice<S: AsRef<str>>(a: &[S], s: &str) -> bool {
    index_of_string(a, s).is_some()
}

/// Removes `s` from `a`, in place.
///
/// This mirrors Go's `RemoveFromStringSlice`, **including its quirk**: the
/// implementation used `if i > 0`, so an element at index `0` (or a missing
/// element) is left untouched. Do not "fix" this without a compatibility
/// decision — callers may rely on it.
pub fn remove_from_string_slice(a: &mut Vec<String>, s: &str) {
    if let Some(index) = a.iter().position(|entry| entry == s) {
        if index > 0 {
            a.remove(index);
        }
    }
}

/// Returns a deep copy of `a`, like Go's `DuplicateStrings`.
pub fn duplicate_strings<S: AsRef<str> + Clone>(a: &[S]) -> Vec<S> {
    a.to_vec()
}

/// Returns whether `a` and `b` contain the same strings in the same order,
/// like Go's `StringSliceEqual`.
pub fn string_slice_equal<A: AsRef<str>, B: AsRef<str>>(a: &[A], b: &[B]) -> bool {
    a.len() == b.len()
        && a.iter()
            .zip(b.iter())
            .all(|(left, right)| left.as_ref() == right.as_ref())
}

/// Returns a deep copy of `a`, like Go's `DuplicateBytes`.
pub fn duplicate_bytes(a: &[u8]) -> Vec<u8> {
    a.to_vec()
}

#[cfg(test)]
mod tests {
    use super::*;

    fn strings() -> Vec<String> {
        ["a", "b", "c", "d", "e", "f", "g", "h", "i", "j"]
            .map(String::from)
            .to_vec()
    }

    #[test]
    fn index_of_string_finds_first_occurrence() {
        let values = strings();
        assert_eq!(index_of_string(&values, "a"), Some(0));
        assert_eq!(index_of_string(&values, "d"), Some(3));
        assert_eq!(index_of_string(&values, "j"), Some(9));
        assert_eq!(index_of_string(&values, "0"), None);
        assert_eq!(index_of_string(&values, "x"), None);
        assert_eq!(index_of_string(&values, "k"), None);
        // Go returns the *first* index.
        assert_eq!(index_of_string(&["x", "a", "x"], "x"), Some(0));
    }

    #[test]
    fn string_in_slice_works() {
        let values = strings();
        for present in ["a", "d", "j"] {
            assert!(string_in_slice(&values, present));
        }
        for absent in ["0", "x", "k"] {
            assert!(!string_in_slice(&values, absent));
        }
        // Also works for `&[&str]`, not just `&[String]`.
        assert!(string_in_slice(&["a", "x"], "x"));
    }

    #[test]
    fn remove_from_string_slice_removes_middle_element() {
        let mut values = strings();
        remove_from_string_slice(&mut values, "b");
        assert!(!string_in_slice(&values, "b"));
        assert_eq!(values.len(), strings().len() - 1);
        // Removing an already removed value is a no-op.
        remove_from_string_slice(&mut values, "b");
        assert_eq!(values.len(), strings().len() - 1);
        // The surviving order is preserved.
        assert_eq!(values[0], "a");
        assert_eq!(values[1], "c");
    }

    #[test]
    fn remove_from_string_slice_keeps_index_zero() {
        // Go's `if i > 0` bug: index 0 is never removed.
        let mut values = strings();
        remove_from_string_slice(&mut values, "a");
        assert_eq!(values.len(), strings().len());
        assert_eq!(values[0], "a");
    }

    #[test]
    fn remove_from_string_slice_keeps_missing_values() {
        let mut values = strings();
        remove_from_string_slice(&mut values, "missing");
        assert_eq!(values, strings());
    }

    #[test]
    fn duplicate_strings_is_a_real_copy() {
        let values = strings();
        let copy = duplicate_strings(&values);
        assert!(string_slice_equal(&copy, &values));
        // Mutating the copy must not touch the original.
        let mut copy = copy;
        copy[0] = String::from("x");
        assert!(!string_slice_equal(&copy, &values));
    }

    #[test]
    fn string_slice_equal_works() {
        let values = strings();
        assert!(string_slice_equal(&values, &values));
        let different_middle = ["a", "x", "c", "d", "e", "f", "g", "h", "i", "j"];
        assert!(!string_slice_equal(&values, &different_middle));
        let shorter = ["a", "x"];
        assert!(!string_slice_equal(&values, &shorter));
        assert!(string_slice_equal::<&str, String>(&[], &[]));
    }

    #[test]
    fn duplicate_bytes_is_a_real_copy() {
        let values: Vec<u8> = (0..10).collect();
        let copy = duplicate_bytes(&values);
        assert_eq!(copy, values);
        let mut copy = copy;
        copy[0] = 0xff;
        assert_ne!(copy, values);
        assert!(duplicate_bytes(&[]).is_empty());
    }
}
