//! Finding the profile of a process.

use crate::fingerprint::{match_fingerprints, parse_fingerprints};
use crate::profile::Profile;
use crate::MatchingData;

/// The result of a profile match.
#[derive(Debug)]
pub struct ProfileMatch<'a> {
    /// The best matching profile.
    pub profile: &'a Profile,
    /// The matching score.
    pub score: i32,
    /// Scoped IDs of profiles with the same score; Go notifies about them.
    pub conflicts: Vec<String>,
}

/// Finds the profile with the highest matching score for the given data.
///
/// Like Go, the first profile wins on a tie; the other candidates are reported
/// as conflicts.
pub fn find_matching_profile<'a>(
    profiles: &'a [Profile],
    md: &dyn MatchingData,
) -> Option<ProfileMatch<'a>> {
    let mut best: Option<&Profile> = None;
    let mut highest_score = 0;
    let mut conflicts = Vec::new();

    for profile in profiles {
        let (prints, _error) = parse_fingerprints(&profile.fingerprints, &profile.linked_path);
        if prints.is_empty() {
            continue;
        }
        let score = match_fingerprints(&prints, md);
        match score {
            0 => {}
            score if score > highest_score => {
                highest_score = score;
                best = Some(profile);
                conflicts.clear();
            }
            score if score == highest_score => {
                if let Some(best) = best {
                    conflicts.push(best.scoped_id());
                }
                conflicts.push(profile.scoped_id());
            }
            _ => {}
        }
    }

    best.map(|profile| ProfileMatch {
        profile,
        score: highest_score,
        conflicts,
    })
}
