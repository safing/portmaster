//! RFC3339 formatting of Go `time.Time` values, as used in JSON records.

/// Formats Unix seconds as RFC3339 in UTC, like Go's `time.Time` JSON form.
///
/// The zero timestamp renders as `0001-01-01T00:00:00Z`, like Go's zero time.
pub fn format_rfc3339(seconds: i64) -> String {
    if seconds <= 0 {
        return "0001-01-01T00:00:00Z".to_string();
    }
    let days = seconds.div_euclid(86_400);
    let second_of_day = seconds.rem_euclid(86_400);
    let (year, month, day) = civil_from_days(days);
    format!(
        "{year:04}-{month:02}-{day:02}T{:02}:{:02}:{:02}Z",
        second_of_day / 3_600,
        (second_of_day % 3_600) / 60,
        second_of_day % 60
    )
}

/// Parses an RFC3339 timestamp into Unix seconds, returning `None` if invalid.
pub fn parse_rfc3339(text: &str) -> Option<i64> {
    if text == "0001-01-01T00:00:00Z" || text.is_empty() {
        return Some(0);
    }
    let (date, clock_with_offset) = text.split_once('T')?;

    let mut clock = clock_with_offset;
    let mut offset_seconds = 0i64;
    if let Some(rest) = clock.strip_suffix('Z') {
        clock = rest;
    } else if let Some(pos) = clock.rfind(['+', '-']) {
        let sign = if clock.as_bytes()[pos] == b'-' { -1 } else { 1 };
        let offset = &clock[pos + 1..];
        let (hours, minutes) = offset.split_once(':')?;
        offset_seconds = sign
            * (hours.parse::<i64>().ok()? * 3_600 + minutes.parse::<i64>().ok()? * 60);
        clock = &clock[..pos];
    }

    let mut date_parts = date.split('-');
    let year: i64 = date_parts.next()?.parse().ok()?;
    let month: u32 = date_parts.next()?.parse().ok()?;
    let day: u32 = date_parts.next()?.parse().ok()?;
    if date_parts.next().is_some() {
        return None;
    }

    let mut time_parts = clock.split(':');
    let hour: i64 = time_parts.next()?.parse().ok()?;
    let minute: i64 = time_parts.next()?.parse().ok()?;
    let seconds_part = time_parts.next()?;
    if time_parts.next().is_some() {
        return None;
    }
    // Drop fractional seconds; malformed short fields must not panic.
    let second: i64 = seconds_part.split('.').next()?.parse().ok()?;

    let days = days_from_civil(year, month, day);
    Some(days * 86_400 + hour * 3_600 + minute * 60 + second - offset_seconds)
}

/// Converts days since the Unix epoch to a civil date (Howard Hinnant).
fn civil_from_days(days: i64) -> (i64, u32, u32) {
    let z = days + 719_468;
    let era = if z >= 0 { z } else { z - 146_096 } / 146_097;
    let day_of_era = (z - era * 146_097) as u64;
    let year_of_era =
        (day_of_era - day_of_era / 1_460 + day_of_era / 36_524 - day_of_era / 146_096) / 365;
    let year = year_of_era as i64 + era * 400;
    let day_of_year = day_of_era - (365 * year_of_era + year_of_era / 4 - year_of_era / 100);
    let mp = (5 * day_of_year + 2) / 153;
    let day = (day_of_year - (153 * mp + 2) / 5 + 1) as u32;
    let month = if mp < 10 { mp + 3 } else { mp - 9 } as u32;
    (if month <= 2 { year + 1 } else { year }, month, day)
}

/// Converts a civil date to days since the Unix epoch.
fn days_from_civil(year: i64, month: u32, day: u32) -> i64 {
    let year = if month <= 2 { year - 1 } else { year };
    let era = if year >= 0 { year } else { year - 399 } / 400;
    let year_of_era = year - era * 400;
    let month = month as i64;
    let day_of_year =
        (153 * (if month > 2 { month - 3 } else { month + 9 }) + 2) / 5 + day as i64 - 1;
    let day_of_era = year_of_era * 365 + year_of_era / 4 - year_of_era / 100 + day_of_year;
    era * 146_097 + day_of_era - 719_468
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn formats_and_parses() {
        assert_eq!(format_rfc3339(0), "0001-01-01T00:00:00Z");
        assert_eq!(format_rfc3339(1_700_000_000), "2023-11-14T22:13:20Z");
        assert_eq!(parse_rfc3339("2023-11-14T22:13:20Z"), Some(1_700_000_000));
        assert_eq!(parse_rfc3339("0001-01-01T00:00:00Z"), Some(0));
        assert_eq!(parse_rfc3339("nonsense"), None);
        for seconds in [1, 1_600_000_000, 1_700_000_000, 2_000_000_000] {
            assert_eq!(parse_rfc3339(&format_rfc3339(seconds)), Some(seconds));
        }
    }

    #[test]
    fn parses_offsets_and_malformed_seconds_without_panicking() {
        // 19:00+07:00 == 12:00Z
        assert_eq!(
            parse_rfc3339("2026-11-17T19:00:00+07:00"),
            parse_rfc3339("2026-11-17T12:00:00Z")
        );
        assert_eq!(
            parse_rfc3339("2026-11-17T12:00:00.123Z"),
            parse_rfc3339("2026-11-17T12:00:00Z")
        );
        assert_eq!(
            parse_rfc3339("2026-11-17T12:00:0Z"),
            parse_rfc3339("2026-11-17T12:00:00Z")
        );
        assert_eq!(parse_rfc3339("2026-11-17T12:00:Z"), None);
        assert_eq!(parse_rfc3339("2026-11-17T12:00:00+bad"), None);
    }
}
