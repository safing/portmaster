//! Pure logic for the counter component.
//!
//! DOM-free and Leptos-free on purpose: the visual layer owns all signals and
//! `view!` macros, while these helpers are plain functions that can be unit
//! tested without a browser or a WASM runtime.

/// Apply a signed delta to the current count.
///
/// Uses checked addition for the normal case and falls back to saturating
/// arithmetic on overflow, so the counter pins to `i32::MIN`/`i32::MAX`
/// instead of wrapping around in release builds.
pub fn step(current: i32, delta: i32) -> i32 {
    current.checked_add(delta).unwrap_or_else(|| current.saturating_add(delta))
}

/// Render a count for display.
pub fn format_count(value: i32) -> String {
    value.to_string()
}

/// Restrict `value` to the inclusive range `[min, max]`.
///
/// Bounds may be supplied in either order; an inverted range is normalized so
/// this helper never panics (unlike [`i32::clamp`], which requires `min <= max`).
pub fn clamp_to_range(value: i32, min: i32, max: i32) -> i32 {
    if min > max {
        value.clamp(max, min)
    } else {
        value.clamp(min, max)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn step_increments() {
        assert_eq!(step(0, 1), 1);
        assert_eq!(step(41, 1), 42);
        assert_eq!(step(-1, 5), 4);
        assert_eq!(step(10, 10), 20);
    }

    #[test]
    fn step_decrements() {
        assert_eq!(step(0, -1), -1);
        assert_eq!(step(1, -1), 0);
        assert_eq!(step(10, -3), 7);
        assert_eq!(step(-5, -5), -10);
    }

    #[test]
    fn step_zero_delta_is_identity() {
        assert_eq!(step(0, 0), 0);
        assert_eq!(step(i32::MAX, 0), i32::MAX);
        assert_eq!(step(i32::MIN, 0), i32::MIN);
    }

    #[test]
    fn step_saturates_on_positive_overflow() {
        assert_eq!(step(i32::MAX, 1), i32::MAX);
        assert_eq!(step(i32::MAX - 1, 5), i32::MAX);
        assert_eq!(step(1, i32::MAX), i32::MAX);
    }

    #[test]
    fn step_saturates_on_negative_overflow() {
        assert_eq!(step(i32::MIN, -1), i32::MIN);
        assert_eq!(step(i32::MIN + 1, -5), i32::MIN);
        assert_eq!(step(-1, i32::MIN), i32::MIN);
    }

    #[test]
    fn format_count_formats_signed_values() {
        assert_eq!(format_count(0), "0");
        assert_eq!(format_count(42), "42");
        assert_eq!(format_count(-1), "-1");
        assert_eq!(format_count(i32::MAX), i32::MAX.to_string());
        assert_eq!(format_count(i32::MIN), i32::MIN.to_string());
    }

    #[test]
    fn clamp_keeps_in_range_values_unchanged() {
        assert_eq!(clamp_to_range(5, 0, 10), 5);
        assert_eq!(clamp_to_range(0, 0, 10), 0);
        assert_eq!(clamp_to_range(10, 0, 10), 10);
    }

    #[test]
    fn clamp_pins_out_of_range_values() {
        assert_eq!(clamp_to_range(-3, 0, 10), 0);
        assert_eq!(clamp_to_range(11, 0, 10), 10);
        assert_eq!(clamp_to_range(i32::MIN, -5, 5), -5);
        assert_eq!(clamp_to_range(i32::MAX, -5, 5), 5);
    }

    #[test]
    fn clamp_normalizes_inverted_bounds() {
        assert_eq!(clamp_to_range(5, 10, 0), 5);
        assert_eq!(clamp_to_range(-3, 10, 0), 0);
        assert_eq!(clamp_to_range(11, 10, 0), 10);
    }
}
