use pm_profile::options::DEFAULT_ACTION_BLOCK_VALUE;
use pm_profile::{
    create_special_profile, is_special_profile_id, special_profile_name, Profile,
    DEFAULT_ACTION_PERMIT_VALUE, SOURCE_LOCAL,
};

#[test]
fn all_special_ids_are_recognized_and_create_profiles() {
    let ids = [
        pm_profile::UNIDENTIFIED_PROFILE_ID,
        pm_profile::UNSOLICITED_PROFILE_ID,
        pm_profile::SYSTEM_PROFILE_ID,
        pm_profile::SYSTEM_RESOLVER_PROFILE_ID,
        pm_profile::PORTMASTER_PROFILE_ID,
        pm_profile::PORTMASTER_APP_PROFILE_ID,
        pm_profile::PORTMASTER_NOTIFIER_PROFILE_ID,
    ];
    for id in ids {
        assert!(is_special_profile_id(id), "{id}");
        assert!(special_profile_name(id).is_some(), "{id}");
        let profile = create_special_profile(id, "/usr/bin/example").unwrap();
        assert_eq!(profile.id, id);
        assert_eq!(profile.source, SOURCE_LOCAL);
        assert_eq!(profile.name, special_profile_name(id).unwrap());
        assert_eq!(profile.presentation_path, "/usr/bin/example");
        // Special profiles have no fingerprints, so an ID is not derived.
        assert!(profile.fingerprints.is_empty());
    }
    assert!(create_special_profile("not-special", "/x").is_none());
}

#[test]
fn special_profile_configs_match_go() {
    let resolver = create_special_profile(pm_profile::SYSTEM_RESOLVER_PROFILE_ID, "/x").unwrap();
    assert_eq!(
        resolver.config["filter"]["defaultAction"],
        DEFAULT_ACTION_PERMIT_VALUE
    );
    assert_eq!(resolver.config["filter"]["blockInternet"], false);
    assert_eq!(resolver.config["filter"]["serviceEndpoints"][0], "+ Localhost");

    let portmaster = create_special_profile(pm_profile::PORTMASTER_PROFILE_ID, "/x").unwrap();
    assert_eq!(
        portmaster.config["filter"]["defaultAction"],
        DEFAULT_ACTION_PERMIT_VALUE
    );
    assert!(portmaster.internal);

    let app = create_special_profile(pm_profile::PORTMASTER_APP_PROFILE_ID, "/x").unwrap();
    assert_eq!(
        app.config["filter"]["defaultAction"],
        DEFAULT_ACTION_BLOCK_VALUE
    );
    assert_eq!(app.config["filter"]["blockInternet"], false);
    assert_eq!(app.config["filter"]["blockLAN"], true);
    assert!(app.internal);

    let unidentified = create_special_profile(pm_profile::UNIDENTIFIED_PROFILE_ID, "/x").unwrap();
    assert!(unidentified.config.is_empty());
    assert!(!unidentified.internal);
}

#[test]
fn special_profile_new_keeps_fixed_id() {
    // Profile::new must not overwrite the explicit ID with a fingerprint hash.
    let profile = Profile::new(Profile {
        id: pm_profile::SYSTEM_RESOLVER_PROFILE_ID.to_string(),
        source: SOURCE_LOCAL.to_string(),
        ..Profile::default()
    });
    assert_eq!(profile.id, pm_profile::SYSTEM_RESOLVER_PROFILE_ID);
}
