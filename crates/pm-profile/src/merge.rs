//! Merging profiles.

use std::sync::Arc;

use crate::fingerprint::{sort_and_compact_fingerprints, Fingerprint};
use crate::icon::sort_and_compact_icons;
use crate::profile::{now, Profile};

/// Merges multiple profiles into a new one, like Go `MergeProfiles`.
///
/// Only icons and fingerprints are inherited; all other information comes from
/// the primary profile. The new profile is returned with an ID derived from the
/// merged fingerprints. Saving it and deleting the old profiles is left to the
/// caller.
pub fn merge_profiles(
    name: &str,
    primary: &Profile,
    secondaries: &[Arc<Profile>],
) -> Result<Profile, String> {
    if secondaries.is_empty() {
        return Err(
            "must supply both a primary and at least one secondary profile for merging".to_string(),
        );
    }

    let now_unix = now();
    let mut merged = Profile {
        id: String::new(),
        source: primary.source.clone(),
        name: name.to_string(),
        description: primary.description.clone(),
        homepage: primary.homepage.clone(),
        use_presentation_path: false,
        config: primary.config.clone(),
        created: now_unix,
        ..Profile::default()
    };
    if merged.name.is_empty() {
        merged.name = primary.name.clone();
    }

    // If any profile was edited, mark the merge as an edit.
    if primary.last_edited > 0 || secondaries.iter().any(|profile| profile.last_edited > 0) {
        merged.last_edited = now_unix;
    }

    // Collect icons.
    merged.icons = primary.icons.clone();
    for secondary in secondaries {
        merged.icons.extend(secondary.icons.iter().cloned());
    }
    sort_and_compact_icons(&mut merged.icons);

    // Collect fingerprints, remembering where they came from.
    merged.fingerprints = merge_fingerprints(&primary.fingerprints, &primary.scoped_id());
    for secondary in secondaries {
        let added = merge_fingerprints(&secondary.fingerprints, &secondary.scoped_id());
        merged.fingerprints.extend(added);
    }
    sort_and_compact_fingerprints(&mut merged.fingerprints);

    Ok(Profile::new(merged))
}

fn merge_fingerprints(fingerprints: &[Fingerprint], from: &str) -> Vec<Fingerprint> {
    fingerprints
        .iter()
        .map(|fingerprint| Fingerprint {
            type_: fingerprint.type_.clone(),
            key: fingerprint.key.clone(),
            operation: fingerprint.operation.clone(),
            value: fingerprint.value.clone(),
            merged_from: from.to_string(),
        })
        .collect()
}
