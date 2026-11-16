//! Line and timestamp formatting, ported from Go `base/log/formatting.go`.
//!
//! Go formats timestamps with `time.Now()` in local time using the layout
//! `2006-01-02 15:04:05.000`. Rust's standard library has no local-time API,
//! so this port formats UTC; see the report for the exact difference.

use std::sync::atomic::{AtomicU16, Ordering};
use std::time::{SystemTime, UNIX_EPOCH};

use crate::message::LogLine;

/// Go time format `2006-01-02 15:04:05.000`, represented as Unix milliseconds.
#[derive(Copy, Clone, Debug, Eq, PartialEq)]
pub struct Timestamp(pub i64);

impl Timestamp {
    /// Current wall-clock time in UTC, with millisecond resolution.
    pub fn now() -> Timestamp {
        let millis = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .map(|d| d.as_millis() as i64)
            .unwrap_or(0);
        Timestamp(millis)
    }

    /// Formats as `2006-01-02 15:04:05.000`, exactly like Go's `timeFormat`.
    pub fn format(self) -> String {
        let (y, m, d, hh, mm, ss, ms) = self.parts();
        format!("{y:04}-{m:02}-{d:02} {hh:02}:{mm:02}:{ss:02}.{ms:03}")
    }

    /// Formats as `2006-01-02_15-04-05`, the UTC file-name format used by Go
    /// `NewFileWriter`.
    pub fn format_filename(self) -> String {
        let (y, m, d, hh, mm, ss, _) = self.parts();
        format!("{y:04}-{m:02}-{d:02}_{hh:02}-{mm:02}-{ss:02}")
    }

    /// Formats as `060102 15:04:05.000`, the layout used by Go
    /// `debug.Info.AddLastUnexpectedLogs` for the "CURRENT TIME" marker.
    pub fn format_go_current(self) -> String {
        let (y, m, d, hh, mm, ss, ms) = self.parts();
        format!("{:02}{:02}{:02} {hh:02}:{mm:02}:{ss:02}.{ms:03}", y % 100, m, d)
    }

    /// Difference in nanoseconds from another timestamp. Can be negative.
    pub fn nanos_since(self, earlier: Timestamp) -> i128 {
        (self.0 as i128 - earlier.0 as i128) * 1_000_000
    }

    fn parts(self) -> (u32, u32, u32, u32, u32, u32, u32) {
        let secs = self.0.div_euclid(1000);
        let ms = self.0.rem_euclid(1000) as u32;
        let days = secs.div_euclid(86_400);
        let sod = secs.rem_euclid(86_400);
        let (year, month, day) = civil_from_days(days);
        let hh = (sod / 3600) as u32;
        let mm = ((sod % 3600) / 60) as u32;
        let ss = (sod % 60) as u32;
        (year as u32, month, day, hh, mm, ss, ms)
    }
}

/// Howard Hinnant's civil-from-days algorithm: days since 1970-01-01 to
/// (year, month, day) in the proleptic Gregorian calendar.
fn civil_from_days(days: i64) -> (i64, u32, u32) {
    let z = days + 719_468;
    let era = if z >= 0 { z } else { z - 146_096 } / 146_097;
    let doe = (z - era * 146_097) as u64;
    let yoe = (doe - doe / 1460 + doe / 36_524 - doe / 146_096) / 365;
    let mut year = yoe as i64 + era * 400;
    let doy = doe - (365 * yoe + yoe / 4 - yoe / 100);
    let mp = (5 * doy + 2) / 153;
    let day = doy - (153 * mp + 2) / 5 + 1;
    let month = if mp < 10 { mp + 3 } else { mp - 9 };
    if month <= 2 {
        year += 1;
    }
    (year, month as u32, day as u32)
}

/// Line counter with Go's `counter`/`maxCount` wrap-around semantics.
#[derive(Debug, Default)]
pub struct LineCounter(AtomicU16);

impl LineCounter {
    /// Returns the next counter value (001..999) and wraps at 999, like Go.
    pub fn next(&self) -> u16 {
        let previous = self.0.load(Ordering::Relaxed);
        let next = previous % 999 + 1;
        self.0.store(next, Ordering::Relaxed);
        next
    }
}

/// Formats a log line, including duplicate count and any attached trace
/// actions, like Go `formatLine`.
pub fn format_line(line: &LogLine, duplicates: u64, use_color: bool, counter: u16) -> String {
    let (color_start, color_end, color_dim, color_end_dim) = if use_color {
        (
            line.level.color(),
            "\x1b[0m",
            "\x1b[2m",
            "\x1b[22m",
        )
    } else {
        ("", "", "", "")
    };

    let mut out = String::new();
    out.push_str(color_dim);
    out.push_str(&line.timestamp.format());
    out.push_str(color_end_dim);
    out.push(' ');
    out.push_str(color_start);
    out.push_str(line.level.short());
    out.push_str(color_end);
    out.push(' ');
    out.push_str(color_dim);

    if line.line == 0 {
        out.push_str("? ");
        out.push_str(super::RIGHT_ARROW);
        out.push(' ');
    } else {
        out.push_str(&short_file(&line.file));
        out.push(':');
        out.push_str(&format!("{:03}", line.line));
        out.push(' ');
        out.push_str(super::RIGHT_ARROW);
        out.push(' ');
    }

    out.push_str(&format!("{counter:03}"));
    out.push_str(&format_duplicates(duplicates));
    out.push_str(color_end_dim);
    out.push(' ');
    out.push_str(&line.msg);

    if let Some(tracer) = &line.tracer {
        if let Some(first) = tracer.first() {
            out.push_str(&format!(" Σ={}", go_duration(line.timestamp.nanos_since(first.timestamp))));
        }

        for (index, action) in tracer.iter().enumerate() {
            let action_color = if use_color { action.level.color() } else { "" };
            let d = match tracer.get(index + 1) {
                Some(next) => next.timestamp.nanos_since(action.timestamp),
                None => line.timestamp.nanos_since(action.timestamp),
            };
            out.push('\n');
            out.push_str(color_dim);
            out.push_str(&format!("{:>23}", go_duration(d)));
            out.push_str(color_end_dim);
            out.push(' ');
            out.push_str(action_color);
            out.push_str(action.level.short());
            out.push_str(color_end);
            out.push(' ');
            out.push_str(color_dim);
            out.push_str(&short_file(&action.file));
            out.push(':');
            out.push_str(&format!("{:03}", action.line));
            out.push(' ');
            out.push_str(super::RIGHT_ARROW);
            out.push_str(color_end_dim);
            out.push_str("     ");
            out.push_str(&action.msg);
        }
    }

    out
}

/// Returns the last 10 characters of a file path, like Go `file[fLen-10:]`.
fn short_file(file: &str) -> String {
    let skip = file.chars().count().saturating_sub(10);
    file.chars().skip(skip).collect()
}

/// Formats the duplicate counter, like Go `formatDuplicates`.
pub fn format_duplicates(duplicates: u64) -> String {
    if duplicates == 0 {
        String::new()
    } else {
        format!(" [{}x]", duplicates + 1)
    }
}

/// Go-style `time.Duration.String()`, used for the `Σ=` and trace-action
/// durations. Supports negative values too (Go can produce them).
pub fn go_duration(nanos: i128) -> String {
    if nanos == 0 {
        return "0s".to_owned();
    }
    let negative = nanos < 0;
    let u = nanos.unsigned_abs();

    let mut body = if u < 1_000_000_000 {
        let (divisor, unit, prec) = if u < 1_000 {
            (1, "ns", 0usize)
        } else if u < 1_000_000 {
            (1_000, "µs", 3usize)
        } else {
            (1_000_000, "ms", 6usize)
        };
        format_scaled(u, divisor, prec, unit)
    } else {
        let seconds = u / 1_000_000_000;
        let frac = u % 1_000_000_000;
        let mut s = if frac == 0 {
            String::new()
        } else {
            let mut frac_str = format!("{frac:09}");
            while frac_str.ends_with('0') {
                frac_str.pop();
            }
            format!(".{frac_str}")
        };
        s.push('s');

        let minutes = seconds / 60;
        let secs = seconds % 60;
        let mut out = format!("{secs}{s}");
        if minutes > 0 {
            let hours = minutes / 60;
            let mins = minutes % 60;
            out = format!("{mins}m{out}");
            if hours > 0 {
                out = format!("{hours}h{out}");
            }
        }
        out
    };

    if negative {
        body.insert(0, '-');
    }
    body
}

fn format_scaled(value: u128, divisor: u128, prec: usize, unit: &str) -> String {
    let whole = value / divisor;
    let frac = value % divisor;
    if frac == 0 || prec == 0 {
        return format!("{whole}{unit}");
    }
    let mut frac_str = format!("{frac:0width$}", width = prec);
    while frac_str.ends_with('0') {
        frac_str.pop();
    }
    format!("{whole}.{frac_str}{unit}")
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::message::LogLine;
    use crate::severity::Severity;

    fn line(msg: &str, level: Severity, file: &str, line_no: u32, millis: i64) -> LogLine {
        LogLine {
            msg: msg.to_owned(),
            level,
            timestamp: Timestamp(millis),
            file: file.to_owned(),
            line: line_no,
            tracer: None,
        }
    }

    #[test]
    fn timestamp_format_matches_go_layout() {
        // 2026-01-02 03:04:05.006 UTC
        let ts = Timestamp(1767323045006);
        assert_eq!(ts.format(), "2026-01-02 03:04:05.006");
        assert_eq!(ts.format_filename(), "2026-01-02_03-04-05");
        assert_eq!(ts.format_go_current(), "260102 03:04:05.006");
    }

    #[test]
    fn counter_wraps_at_999_like_go() {
        let counter = LineCounter::default();
        assert_eq!(counter.next(), 1);
        for _ in 0..997 {
            counter.next();
        }
        assert_eq!(counter.next(), 999);
        assert_eq!(counter.next(), 1);
    }

    #[test]
    fn plain_line_format_matches_go() {
        let ll = line("hello", Severity::Info, "/path/to/file.go", 42, 1767323045006);
        let out = format_line(&ll, 0, false, 7);
        assert_eq!(out, "2026-01-02 03:04:05.006 INF to/file.go:042 ▶ 007 hello");
        assert_eq!(
            format_line(&ll, 2, false, 8),
            "2026-01-02 03:04:05.006 INF to/file.go:042 ▶ 008 [3x] hello"
        );
    }

    #[test]
    fn line_without_file_uses_question_mark() {
        let ll = line("boot", Severity::Info, "", 0, 1767323045006);
        assert_eq!(
            format_line(&ll, 0, false, 1),
            "2026-01-02 03:04:05.006 INF ? ▶ 001 boot"
        );
    }

    #[test]
    fn colored_line_uses_ansi_codes() {
        let ll = line("boom", Severity::Error, "x.go", 1, 1767323045006);
        let out = format_line(&ll, 0, true, 1);
        assert!(out.starts_with("\x1b[2m2026-01-02 03:04:05.006\x1b[22m \x1b[91mERR\x1b[0m"));
        assert!(out.contains("\x1b[2mx.go:001 ▶"));
    }

    #[test]
    fn trace_actions_are_appended() {
        let mut main = line("done", Severity::Info, "main.go", 10, 1767323045500);
        main.tracer = Some(vec![
            line("step one", Severity::Trace, "a.go", 1, 1767323045000),
            line("step two", Severity::Warning, "b.go", 2, 1767323045200),
        ]);
        let out = format_line(&main, 0, false, 1);
        assert!(out.contains("Σ=500ms"), "{out}");
        assert!(out.contains("TRC a.go:001 ▶     step one"), "{out}");
        assert!(out.contains("WRN b.go:002 ▶     step two"), "{out}");
    }

    #[test]
    fn duration_format_matches_go() {
        assert_eq!(go_duration(0), "0s");
        assert_eq!(go_duration(1), "1ns");
        assert_eq!(go_duration(1_500), "1.5µs");
        assert_eq!(go_duration(10_050), "10.05µs");
        assert_eq!(go_duration(5_000_000), "5ms");
        assert_eq!(go_duration(1_500_000_000), "1.5s");
        assert_eq!(go_duration(-2_000_000), "-2ms");
    }
}
