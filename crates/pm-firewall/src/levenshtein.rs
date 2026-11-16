//! Port of `levenshtein.Match` of github.com/agext/levenshtein with default
//! parameters (unit costs, prefix bonus up to 4 characters, scale 0.1,
//! threshold 0.7).

const BONUS_PREFIX: usize = 4;
const BONUS_SCALE: f64 = 0.1;
const BONUS_THRESHOLD: f64 = 0.7;

/// Returns a similarity in 0..=1 with a Winkler-style common prefix bonus.
pub fn match_score(str1: &str, str2: &str) -> f64 {
    let s1: Vec<char> = str1.chars().collect();
    let s2: Vec<char> = str2.chars().collect();
    if s1.is_empty() && s2.is_empty() {
        return 1.0;
    }
    let max_dist = s1.len().max(s2.len());

    let prefix = s1.iter().zip(&s2).take_while(|(a, b)| a == b).count();
    let dist = distance(&s1, &s2);
    let mut sim = 1.0 - dist as f64 / max_dist as f64;
    if (BONUS_THRESHOLD..1.0).contains(&sim) {
        sim += prefix.min(BONUS_PREFIX) as f64 * BONUS_SCALE * (1.0 - sim);
    }
    sim
}

fn distance(a: &[char], b: &[char]) -> usize {
    let mut row: Vec<usize> = (0..=b.len()).collect();
    for (i, ca) in a.iter().enumerate() {
        let mut diag = row[0];
        row[0] = i + 1;
        for (j, cb) in b.iter().enumerate() {
            let up = row[j + 1];
            let cost = usize::from(ca != cb);
            row[j + 1] = (diag + cost).min(up + 1).min(row[j] + 1);
            diag = up;
        }
    }
    row[b.len()]
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn identical_and_empty() {
        assert_eq!(match_score("", ""), 1.0);
        assert_eq!(match_score("abc", "abc"), 1.0);
        assert_eq!(match_score("abc", ""), 0.0);
    }

    #[test]
    fn prefix_bonus_applies_over_threshold() {
        // dist 1 of 5: sim 0.8, prefix 4 -> 0.8 + 4*0.1*0.2 = 0.88
        let score = match_score("abcde", "abcdx");
        assert!((score - 0.88).abs() < 1e-9, "{score}");
        // no common prefix, no bonus: dist 1 of 5
        let score = match_score("xbcde", "abcde");
        assert!((score - 0.8).abs() < 1e-9, "{score}");
    }

    #[test]
    fn below_threshold_has_no_bonus() {
        // "kitten"/"sitting": dist 3 of 7
        let score = match_score("kitten", "sitting");
        assert!((score - (1.0 - 3.0 / 7.0)).abs() < 1e-9, "{score}");
    }
}
