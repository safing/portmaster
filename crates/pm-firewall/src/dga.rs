//! Port of Go `service/detection/dga` (longest meaningful substring score).

/// Mean longest meaningful substring score of a dotted domain, weighted by
/// label length. Returns 0..=100.
pub fn lms_score_of_domain(domain: &str) -> f64 {
    let domain = domain.to_lowercase();
    let subjects: Vec<&str> = domain.split('.').collect();
    let total_length: usize = subjects.iter().map(|s| s.len()).sum();
    let mut total_score = 0.0;
    for subject in subjects {
        if !subject.is_empty() {
            total_score += lms_score(subject) * (subject.len() as f64 / total_length as f64);
        }
    }
    total_score
}

/// Percentage (0..=100) of the longest run of `a-z` in the subject.
///
/// Like Go, a run that reaches the end of the subject only counts when no
/// other run was terminated before it; positions are byte offsets.
pub fn lms_score(subject: &str) -> f64 {
    let mut lms_start: Option<usize> = None;
    let mut lms_stop: Option<usize> = None;
    let mut longest = 0usize;

    for (i, c) in subject.char_indices() {
        if c.is_ascii_lowercase() {
            if lms_start.is_none() {
                lms_start = Some(i);
            }
        } else if let Some(start) = lms_start {
            lms_stop = Some(i);
            if i - start > longest {
                longest = i - start;
            }
            lms_start = None;
        }
    }
    if lms_stop.is_none() {
        longest = subject.len();
    }
    longest as f64 * 100.0 / subject.len() as f64
}

#[cfg(test)]
mod tests {
    use super::*;

    fn check(domain: &str, min: f64, max: f64) {
        let score = lms_score_of_domain(domain);
        assert!(
            score >= min && score <= max,
            "domain {domain} scored {score:.2}, expected {min}..={max}"
        );
    }

    #[test]
    fn go_test_cases() {
        check("g.symcd.com.", 100.0, 100.0);
        check("www.google.com.", 100.0, 100.0);
        check("55ttt5.12abc3.test.com.", 68.0, 69.0);
        check(
            "mbtq6opnuodp34gcrma65fxacgxv5ukr7lq6xuhr4mhoibe7.yvqptrozfbnqyemchpovw3q5xwjibuxfsgb72mix3znhpfhc.i2n7jh2gadqaadck3zs3vg3hbv5pkmwzeay4gc75etyettbb.isi5mhmowtfriu33uxzmgvjur5g2p3tloynwohfrggee6fkn.meop7kqyd5gwxxa3.er.spotify.com.",
            0.0,
            31.0,
        );
    }

    #[test]
    fn empty_subject_is_nan_like_go() {
        assert!(lms_score("").is_nan());
    }
}
