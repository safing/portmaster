//! Time helpers.

use std::time::{SystemTime, UNIX_EPOCH};

/// Current time as Unix seconds.
pub fn now() -> i64 {
    now_with_nanos().0
}

fn now_with_nanos() -> (i64, u32) {
    match SystemTime::now().duration_since(UNIX_EPOCH) {
        Ok(elapsed) => (elapsed.as_secs() as i64, elapsed.subsec_nanos()),
        Err(_) => (0, 0),
    }
}

/// Formats the current time like the Go sqlite driver with
/// `_time_format=sqlite`: `2006-01-02 15:04:05.999999999-07:00` in UTC.
pub fn utc_timestamp_now() -> String {
    let (secs, nanos) = now_with_nanos();
    let days = secs.div_euclid(86_400);
    let second_of_day = secs.rem_euclid(86_400);
    let (year, month, day) = civil_from_days(days);
    let hour = second_of_day / 3_600;
    let minute = (second_of_day % 3_600) / 60;
    let second = second_of_day % 60;

    let mut out = format!("{year:04}-{month:02}-{day:02} {hour:02}:{minute:02}:{second:02}");
    if nanos > 0 {
        let mut fraction = format!("{nanos:09}");
        while fraction.ends_with('0') {
            fraction.pop();
        }
        out.push('.');
        out.push_str(&fraction);
    }
    out.push_str("+00:00");
    out
}

/// Converts days since the Unix epoch to a civil date (Howard Hinnant's algorithm).
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

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn civil_dates() {
        assert_eq!(civil_from_days(0), (1970, 1, 1));
        assert_eq!(civil_from_days(19_000), (2022, 1, 8));
        assert_eq!(civil_from_days(20_000), (2024, 10, 4));
        assert_eq!(civil_from_days(-1), (1969, 12, 31));
    }

    #[test]
    fn timestamp_shape() {
        let ts = utc_timestamp_now();
        assert!(ts.len() >= 25, "{ts}");
        assert!(ts.ends_with("+00:00"), "{ts}");
        assert_eq!(&ts[4..5], "-");
        assert_eq!(&ts[13..14], ":");
    }
}
