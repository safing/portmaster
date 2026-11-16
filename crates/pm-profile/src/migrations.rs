//! Port of Go `service/profile/migrations.go` on top of `pm-migration`.
//!
//! The three migrations are applied once per database, in increasing version
//! order, and the last successful version is stored under
//! `core:migrations/profile` — the same record the Go core reads.

use pm_database::Interface;
use pm_migration::{Migration, Registry};
use pm_query::Query;

use crate::fingerprint::{FINGERPRINT_OPERATION_EQUALS, FINGERPRINT_TYPE_PATH};
use crate::profile::Profile;
use crate::{derive_profile_id, make_scoped_id, Icon, PROFILES_DB_PATH, SOURCE_LOCAL};

/// Database key holding the migration status.
pub const MIGRATION_KEY: &str = "core:migrations/profile";

/// Builds the profile migration registry.
pub fn registry() -> Result<Registry, pm_migration::Error> {
    let mut registry = Registry::new(MIGRATION_KEY);
    registry.add(vec![
        Migration::new(
            "Migrate from LinkedPath to Fingerprints and PresentationPath",
            "v0.9.9",
            migrate_linked_path,
        )?,
        Migration::new(
            "Migrate from Icon Fields to Icon List",
            "v1.4.7",
            migrate_icons,
        )?,
        Migration::new(
            "Migrate from random profile IDs to fingerprint-derived IDs",
            "v1.6.3",
            migrate_to_derived_ids,
        )?,
    ])?;
    Ok(registry)
}

/// Runs all pending profile migrations.
pub fn migrate(db: &Interface) -> Result<pm_migration::Diagnostics, pm_migration::Error> {
    registry()?.migrate(db)
}

fn profiles(db: &Interface) -> Result<Vec<Profile>, String> {
    let records = db
        .query(&Query::new(PROFILES_DB_PATH))
        .map_err(|error| format!("failed to start query: {error}"))?;
    let mut result = Vec::with_capacity(records.len());
    for record in &records {
        match serde_json::from_slice::<Profile>(&record.data) {
            Ok(profile) => result.push(profile),
            Err(error) => tracing::debug!("failed to parse profile for migration: {error}"),
        }
    }
    Ok(result)
}

fn save(db: &Interface, profile: &Profile) -> Result<(), String> {
    db.put(crate::store::encode_profile(profile).map_err(|error| error.to_string())?)
        .map_err(|error| error.to_string())
}

fn migrate_linked_path(db: &Interface, _from: &pm_updates::Version, _to: &pm_updates::Version) -> Result<(), String> {
    for mut profile in profiles(db)? {
        if profile.linked_path.is_empty() {
            continue;
        }
        let mut changed = false;
        if profile.presentation_path.is_empty() {
            profile.presentation_path = profile.linked_path.clone();
            changed = true;
        }
        if profile.name.is_empty() && !profile.presentation_path.is_empty() {
            profile.name = binary_name_from_path(&profile.presentation_path);
            changed = true;
        }
        if profile.fingerprints.is_empty() {
            profile.fingerprints.push(crate::Fingerprint::new(
                FINGERPRINT_TYPE_PATH,
                "",
                FINGERPRINT_OPERATION_EQUALS,
                &profile.linked_path,
            ));
            changed = true;
        }
        if profile.linked_path.is_empty() && !profile.presentation_path.is_empty() {
            profile.linked_path = profile.presentation_path.clone();
            changed = true;
        }
        if changed && profile.source == SOURCE_LOCAL {
            save(db, &profile)?;
        }
    }
    Ok(())
}

fn migrate_icons(db: &Interface, _from: &pm_updates::Version, _to: &pm_updates::Version) -> Result<(), String> {
    let mut failed = 0usize;
    let mut total = 0usize;
    let mut last_error = String::new();
    for mut profile in profiles(db)? {
        if profile.icon.is_empty() || profile.icon_type.is_empty() || !profile.icons.is_empty() {
            continue;
        }
        profile.icons.push(Icon::new(&profile.icon_type, &profile.icon, ""));
        total += 1;
        if let Err(error) = save(db, &profile) {
            failed += 1;
            last_error = error;
        }
    }
    if !last_error.is_empty() {
        return Err(format!(
            "failed to migrate {failed} profiles (out of {total} pending) - last error: {last_error}"
        ));
    }
    Ok(())
}

fn looks_like_uuid(id: &str) -> bool {
    let bytes = id.as_bytes();
    if bytes.len() != 36 {
        return false;
    }
    for (index, byte) in bytes.iter().enumerate() {
        if matches!(index, 8 | 13 | 18 | 23) {
            if *byte != b'-' {
                return false;
            }
        } else if !byte.is_ascii_hexdigit() {
            return false;
        }
    }
    true
}

fn migrate_to_derived_ids(db: &Interface, _from: &pm_updates::Version, _to: &pm_updates::Version) -> Result<(), String> {
    let mut to_delete = Vec::new();
    let mut failed = 0usize;
    let mut total = 0usize;
    let mut last_error = String::new();

    for mut profile in profiles(db)? {
        if !looks_like_uuid(&profile.id) {
            continue;
        }
        let old_scoped_id = make_scoped_id(&profile.source, &profile.id);
        let new_id = derive_profile_id(&mut profile.fingerprints);
        if profile.id == new_id {
            continue;
        }
        profile.id = new_id;
        total += 1;
        if let Err(error) = save(db, &profile) {
            failed += 1;
            last_error = error;
            continue;
        }
        to_delete.push(old_scoped_id);
    }

    for scoped_id in to_delete {
        if let Err(error) = db.delete(&format!("{PROFILES_DB_PATH}{scoped_id}")) {
            tracing::warn!("failed to delete old profile {scoped_id} during migration: {error}");
        }
    }
    if !last_error.is_empty() {
        return Err(format!(
            "failed to migrate {failed} profiles (out of {total} pending) - last error: {last_error}"
        ));
    }
    Ok(())
}

/// Minimal equivalent of Go `binmeta.GenerateBinaryNameFromPath` for the
/// migration: prettify the final path component.
fn binary_name_from_path(path: &str) -> String {
    let name = path.rsplit(['/', '\\']).next().unwrap_or(path);
    let name = name.trim_end_matches(".exe");
    let mut result = String::new();
    for character in name.chars() {
        if character == '-' || character == '_' {
            result.push(' ');
        } else {
            result.push(character);
        }
    }
    let mut chars = result.chars();
    match chars.next() {
        Some(first) => first.to_uppercase().collect::<String>() + chars.as_str(),
        None => String::new(),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn uuid_detection_matches_go_regex() {
        assert!(looks_like_uuid("550e8400-e29b-41d4-a716-446655440000"));
        assert!(!looks_like_uuid("PTYGhKL6HvJhjCQU3vkGFstsXZynfyir8vo9ii2gE62vw1"));
        assert!(!looks_like_uuid("550e8400e29b41d4a716446655440000"));
    }

    #[test]
    fn generates_readable_binary_names() {
        assert_eq!(binary_name_from_path("/usr/bin/avahi-daemon"), "Avahi daemon");
        assert_eq!(binary_name_from_path("C:\\Program Files\\App\\tool.exe"), "Tool");
    }

    #[test]
    fn registry_has_go_versions() {
        let registry = registry().unwrap();
        let versions = registry
            .migrations()
            .iter()
            .map(|migration| migration.version.to_string())
            .collect::<Vec<_>>();
        assert_eq!(versions, vec!["0.9.9", "1.4.7", "1.6.3"]);
    }
}
