//! Tests for the Go-compatible glob matcher.

use pm_updates::{Glob, GlobError};

fn matches(pattern: &str, path: &str) -> bool {
    Glob::compile(pattern).expect("compile glob").is_match(path)
}

#[test]
fn star_does_not_cross_separators() {
    assert!(matches("*.json", "portmaster.json"));
    assert!(!matches("*.json", "linux_amd64/index.json"));
    assert!(matches("*", "portmaster.json"));
    assert!(!matches("*", "a/b"));
    assert!(matches("linux_amd64/*", "linux_amd64/portmaster-core"));
    assert!(matches("linux_amd64/*", "linux_amd64/portmaster-core-rs"));
}

#[test]
fn double_star_crosses_separators() {
    // `**` may be empty, but the literal `/` after it still has to match.
    assert!(!matches("**/*.sig", "portmaster-core.sig"));
    assert!(matches("**/*.sig", "linux_amd64/portmaster-core.sig"));
    assert!(matches("**/x", "a/x"));
    assert!(!matches("**/x", "x"));
    assert!(matches("latest/**", "latest/linux_amd64/index.json"));
    assert!(matches("**/app2/**/portmaster-app*.zip", "x/app2/a/b/portmaster-app-1.zip"));
    assert!(!matches("**/*.apk", "portmaster-core"));
}

#[test]
fn patterns_from_the_update_scanner() {
    // These are the patterns of the real `updatemgr scan` configuration.
    let ignored = [
        ("*.json", "sha256sums.txt", false),
        ("*.json", "index.json", true),
        ("sha256*.txt", "sha256sums.txt", true),
        ("latest/**", "latest/index.json", true),
        ("**/*.apk", "platforms/android/portmaster.apk", true),
        ("**/*install*", "windows/portmaster-installer.exe", true),
        ("**/spn-hub*", "spn-hub-linux", false),
        ("**/spn-hub*", "linux_amd64/spn-hub-linux", true),
        ("darwin_amd64/**", "darwin_amd64/portmaster-core", true),
        ("darwin_arm64/**", "linux_amd64/portmaster-core", false),
        ("**/portmaster-start*", "linux_amd64/portmaster-start-app", true),
        ("**/profilemgr*.zip", "linux_amd64/profilemgr.zip", true),
    ];
    for (pattern, path, expected) in ignored {
        assert_eq!(matches(pattern, path), expected, "{pattern} vs {path}");
    }

    let unpack = [("**/*.gz", "linux_amd64/intel/geoipv4.mmdb.gz", true)];
    for (pattern, path, expected) in unpack {
        assert_eq!(matches(pattern, path), expected, "{pattern} vs {path}");
    }
}

#[test]
fn character_classes_and_escapes() {
    assert!(matches("file[0-9].txt", "file7.txt"));
    assert!(!matches("file[0-9].txt", "filex.txt"));
    assert!(matches("file[!0-9].txt", "filex.txt"));
    // gobwas has no `^` negation.
    assert!(!matches("file[^0-9].txt", "filex.txt"));
    assert!(matches("file[^0-9].txt", "file^.txt"));
    assert!(matches("literal\\*star", "literal*star"));
    assert!(!matches("literal\\*star", "literalXstar"));
    assert!(matches("a?c", "abc"));
    assert!(!matches("a?c", "a/c"));
}

#[test]
fn unsupported_patterns_are_rejected() {
    assert_eq!(Glob::compile("{a,b}"), Err(GlobError::Unsupported("{".to_string())));
    assert_eq!(Glob::compile("abc[a-"), Err(GlobError::UnclosedClass));
    assert_eq!(Glob::compile("abc\\"), Err(GlobError::TrailingEscape));
}
