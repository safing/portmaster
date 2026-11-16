//! Tests of the concrete Linux tag handlers and their integration with the
//! process cache and profile matching. Expected values are taken from Go's
//! `service/process/tags` handlers.

mod common;

use pm_process::tags::{
    appimage::AppImageHandler, flatpak::FlatpakHandler, interpreter::InterpHandler, net::NetworkHandler,
    snap::SnapHandler,
};
use pm_process::{Process, ProcessCache, Tag, TagHandler};
use pm_profile::{Fingerprint, Profile, FINGERPRINT_OPERATION_EQUALS, FINGERPRINT_TYPE_TAG, SOURCE_LOCAL};

fn process(path: &str, cmdline: &str, cwd: &str) -> Process {
    let mut process = Process::empty("test-1");
    process.path = path.to_string();
    process.cmd_line = cmdline.to_string();
    process.cwd = cwd.to_string();
    process
}

fn tag<'a>(process: &'a Process, key: &str) -> Option<&'a str> {
    process.tag(key).map(|tag| tag.value.as_str())
}

// ---------------------------------------------------------------------------
// Interpreter
// ---------------------------------------------------------------------------

#[test]
fn interpreter_tags_absolute_script() {
    let root = common::temp_dir("tags-interp-abs");
    let script = root.join("script.py");
    common::write_file(&script, "#!/usr/bin/python3\nprint('héllo')\n");
    let script = script.to_string_lossy().into_owned();

    let mut p = process(
        "/usr/bin/python3",
        &format!("/usr/bin/python3 {script} --flag"),
        "/home/test",
    );
    InterpHandler.add_tags(&mut p);

    assert_eq!(tag(&p, "python-script"), Some(script.as_str()));
    assert_eq!(p.matching_path, script);
}

#[test]
fn interpreter_joins_relative_script_with_cwd() {
    let root = common::temp_dir("tags-interp-rel");
    common::write_file(&root.join("script.py"), "print(1)\n");
    let cwd = root.to_string_lossy().into_owned();

    let mut p = process("/usr/bin/python3", "python3 ./script.py", &cwd);
    // argv[0] does not match the interpreter regex, so it is tagged directly.
    InterpHandler.add_tags(&mut p);
    assert_eq!(tag(&p, "python-script"), Some("python3"));
    assert_eq!(p.matching_path, "python3");

    let mut p = process("/usr/bin/python3", "/usr/bin/python3 ./script.py", &cwd);
    InterpHandler.add_tags(&mut p);
    let expected = root.join("script.py").to_string_lossy().into_owned();
    assert_eq!(tag(&p, "python-script"), Some(expected.as_str()));
    assert_eq!(p.matching_path, expected);
}

#[test]
fn interpreter_handles_quoted_paths_and_shell() {
    let root = common::temp_dir("tags-interp-quote");
    let script = root.join("my script.sh");
    common::write_file(&script, "#!/bin/bash\necho hi\n");
    let script = script.to_string_lossy().into_owned();

    let mut p = process("/bin/bash", &format!("/bin/bash \"{script}\""), "/home/test");
    InterpHandler.add_tags(&mut p);
    assert_eq!(tag(&p, "shell-script"), Some(script.as_str()));
    assert_eq!(p.matching_path, script);
}

#[test]
fn interpreter_skips_interactive_non_utf8_and_unknown() {
    // Interactive session: argv[0] is the interpreter with no script.
    let mut p = process("/usr/bin/python3", "/usr/bin/python3", "/home/test");
    InterpHandler.add_tags(&mut p);
    assert!(p.tags.is_empty());

    // Non-UTF-8 script file.
    let root = common::temp_dir("tags-interp-binary");
    let script = root.join("bad.py");
    common::write_file(&script, [0xff, 0xfe, 0x00, 0x01]);
    let mut p = process(
        "/usr/bin/python3",
        &format!("/usr/bin/python3 {}", script.to_string_lossy()),
        "/home/test",
    );
    InterpHandler.add_tags(&mut p);
    assert!(p.tags.is_empty());

    // Unknown interpreter binary.
    let mut p = process("/usr/bin/foo", "/usr/bin/foo /tmp/a.py", "/home/test");
    InterpHandler.add_tags(&mut p);
    assert!(p.tags.is_empty());

    // Plain `python` (without a version) is not matched, like the Go regex.
    let mut p = process("/usr/bin/python", "/usr/bin/python /tmp/a.py", "/home/test");
    InterpHandler.add_tags(&mut p);
    assert!(p.tags.is_empty());
}

#[test]
fn interpreter_recognizes_all_known_binaries() {
    let cases = [
        ("/bin/python2", "python-script"),
        ("/usr/bin/python3.11", "python-script"),
        ("/bin/sh", "shell-script"),
        ("/usr/bin/bash", "shell-script"),
        ("/bin/zsh", "shell-script"),
        ("/bin/perl", "perl-script"),
        ("/usr/bin/ruby", "ruby-script"),
        ("/bin/node", "nodejs-script"),
        ("/usr/bin/nodejs", "nodejs-script"),
    ];
    for (path, expected) in cases {
        let mut p = process(path, "something", "/home/test");
        InterpHandler.add_tags(&mut p);
        assert_eq!(p.tags.len(), 1, "path {path}");
        assert_eq!(p.tags[0].key, expected, "path {path}");
        assert_eq!(p.tags[0].value, "something", "path {path}");
    }
}

// ---------------------------------------------------------------------------
// AppImage
// ---------------------------------------------------------------------------

#[test]
fn appimage_tags_from_env() {
    let root = common::temp_dir("tags-appimage-env");
    let mounts = root.join("mounts");
    common::write_file(&mounts, "");
    let handler = AppImageHandler::with_mounts_path(&mounts);

    let mut p = process("/tmp/.mount_ABC/usr/bin/app", "app", "/home/test");
    p.env.insert("APPIMAGE".into(), "/home/user/App.AppImage".into());
    p.env.insert("APPDIR".into(), "/tmp/.mount_ABC".into());
    handler.add_tags(&mut p);

    assert_eq!(tag(&p, "app-image-path"), Some("/home/user/App.AppImage"));
    assert_eq!(p.matching_path, "/home/user/App.AppImage");
}

#[test]
fn appimage_env_requires_path_inside_mount_dir() {
    let root = common::temp_dir("tags-appimage-mismatch");
    let mounts = root.join("mounts");
    common::write_file(&mounts, "");
    let handler = AppImageHandler::with_mounts_path(&mounts);

    let mut p = process("/usr/bin/app", "app", "/home/test");
    p.env.insert("APPIMAGE".into(), "/home/user/App.AppImage".into());
    p.env.insert("APPDIR".into(), "/tmp/.mount_ABC".into());
    handler.add_tags(&mut p);

    assert!(p.tags.is_empty());
    assert!(p.matching_path.is_empty());
}

#[test]
fn appimage_tags_mount_id_from_proc_mounts() {
    let cases = [
        ("Firefox.AppImage", "Firefox"),
        ("gearlever_Firefox.AppImage", "Firefox"),
        ("AppImage123.AppImage", "AppImage123"),
    ];
    for (source, expected) in cases {
        let root = common::temp_dir("tags-appimage-mount");
        let mounts = root.join("mounts");
        common::write_file(
            &mounts,
            format!("overlay / other rw 0 0\n{source} /tmp/.mount_AbCd12 fuse.rofs rw 0 0\n"),
        );
        let handler = AppImageHandler::with_mounts_path(&mounts);

        let mut p = process("/tmp/.mount_AbCd12/AppRun", "AppRun", "/home/test");
        handler.add_tags(&mut p);
        assert_eq!(tag(&p, "app-image-mount-id"), Some(expected), "source {source}");
    }

    // A mount source without the .appimage suffix is ignored.
    let root = common::temp_dir("tags-appimage-mount-bad");
    let mounts = root.join("mounts");
    common::write_file(&mounts, "/dev/loop0 /tmp/.mount_AbCd12 ext4 rw 0 0\n");
    let handler = AppImageHandler::with_mounts_path(&mounts);
    let mut p = process("/tmp/.mount_AbCd12/AppRun", "AppRun", "/home/test");
    handler.add_tags(&mut p);
    assert!(p.tags.is_empty());
}

// ---------------------------------------------------------------------------
// Snap
// ---------------------------------------------------------------------------

#[test]
fn snap_tags_from_env() {
    let mut p = process("/snap/foo/123/bin/foo", "foo", "/home/test");
    p.env.insert("SNAP".into(), "/snap/foo/123".into());
    p.env.insert("SNAP_NAME".into(), "foo".into());
    p.env.insert("SNAP_VERSION".into(), "1.2.3".into());
    SnapHandler.add_tags(&mut p);
    assert_eq!(tag(&p, "snap-name"), Some("foo"));
    assert_eq!(tag(&p, "snap-version"), Some("1.2.3"));
}

#[test]
fn snap_falls_back_to_path() {
    // SNAP env points elsewhere; path parsing still yields the name.
    let mut p = process("/snap/foo/123/bin/foo", "foo", "/home/test");
    p.env.insert("SNAP".into(), "/snap/other/1".into());
    SnapHandler.add_tags(&mut p);
    assert_eq!(tag(&p, "snap-name"), Some("foo"));
    assert_eq!(tag(&p, "snap-version"), None);

    // No env at all.
    let mut p = process("/snap/bar/7/usr/bin/tool", "tool", "/home/test");
    SnapHandler.add_tags(&mut p);
    assert_eq!(tag(&p, "snap-name"), Some("bar"));

    // Outside /snap/ and no env: nothing.
    let mut p = process("/usr/bin/tool", "tool", "/home/test");
    SnapHandler.add_tags(&mut p);
    assert!(p.tags.is_empty());
}

#[test]
fn snap_env_needs_both_name_and_version() {
    let mut p = process("/snap/foo/123/bin/foo", "foo", "/home/test");
    p.env.insert("SNAP".into(), "/snap/foo/123".into());
    p.env.insert("SNAP_NAME".into(), "foo".into());
    SnapHandler.add_tags(&mut p);
    assert_eq!(tag(&p, "snap-name"), Some("foo"));
    assert_eq!(tag(&p, "snap-version"), None);
}

// ---------------------------------------------------------------------------
// Flatpak
// ---------------------------------------------------------------------------

#[test]
fn flatpak_tags_app_space() {
    let mut p = process("/app/bin/foo", "foo", "/home/test");
    p.env.insert("FLATPAK_ID".into(), "org.example.Foo".into());
    FlatpakHandler.add_tags(&mut p);
    assert_eq!(tag(&p, "flatpak-id"), Some("org.example.Foo"));
    assert_eq!(p.matching_path, "/app/bin/foo");
}

#[test]
fn flatpak_requires_app_path_and_nonempty_id() {
    let mut p = process("/usr/bin/foo", "foo", "/home/test");
    p.env.insert("FLATPAK_ID".into(), "org.example.Foo".into());
    FlatpakHandler.add_tags(&mut p);
    assert!(p.tags.is_empty());

    let mut p = process("/app/bin/foo", "foo", "/home/test");
    p.env.insert("FLATPAK_ID".into(), String::new());
    FlatpakHandler.add_tags(&mut p);
    assert!(p.tags.is_empty());
}

// ---------------------------------------------------------------------------
// Network
// ---------------------------------------------------------------------------

#[test]
fn network_handler_is_a_noop_and_exposes_ip_tag() {
    let descriptions = NetworkHandler.tag_descriptions();
    assert_eq!(descriptions.len(), 1);
    assert_eq!(descriptions[0].id, "ip");
    assert_eq!(descriptions[0].name, "IP Address");
    assert_eq!(
        descriptions[0].description,
        "The remote IP address of external requests to Portmaster, if enabled."
    );

    let mut p = process("/usr/bin/foo", "foo", "/home/test");
    p.tags.push(Tag {
        key: "ip".into(),
        value: "1.2.3.4".into(),
    });
    NetworkHandler.add_tags(&mut p);
    assert_eq!(p.tags.len(), 1);
}

// ---------------------------------------------------------------------------
// Registration / integration
// ---------------------------------------------------------------------------

#[test]
fn default_handlers_are_registered_and_applied() {
    let root = common::temp_dir("tags-integration");
    common::set_boot_time(&root, 1_700_000_000);
    common::add_process(&root, 2000, 1, 2000, 100, "flatpak-app", "/app/bin/foo");
    let environ = root.join("2000/environ");
    common::write_file(&environ, b"FLATPAK_ID=org.example.Foo\0HOME=/home/test\0");
    let passwd = common::write_passwd(&root);

    let cache = ProcessCache::with_default_tag_handlers(
        pm_process::ProcReader::with_passwd(pm_netstate::ProcFs::new(&root), &passwd),
    );

    let descriptions = cache.tag_descriptions();
    let ids: Vec<&str> = descriptions
        .iter()
        .map(|description| description.id.as_str())
        .collect();
    assert_eq!(ids.len(), 11);
    for id in [
        "app-image-path",
        "app-image-mount-id",
        "flatpak-id",
        "python-script",
        "shell-script",
        "perl-script",
        "ruby-script",
        "nodejs-script",
        "ip",
        "snap-name",
        "snap-version",
    ] {
        assert!(ids.contains(&id), "missing tag description {id}");
    }

    let process = cache.get_or_find(2000).expect("process 2000");
    assert_eq!(tag(&process, "flatpak-id"), Some("org.example.Foo"));
    assert_eq!(process.matching_path, "/app/bin/foo");

    // Tags produced during attribution feed into profile matching.
    let profile = Profile::new(Profile {
        source: SOURCE_LOCAL.to_string(),
        name: "flatpak app".to_string(),
        fingerprints: vec![Fingerprint::new(
            FINGERPRINT_TYPE_TAG,
            "flatpak-id",
            FINGERPRINT_OPERATION_EQUALS,
            "org.example.Foo",
        )],
        ..Profile::default()
    });
    let profiles = [profile];
    let matched = process.find_profile(&profiles).expect("a profile");
    assert_eq!(matched.profile.name, "flatpak app");
}

#[test]
fn default_handlers_can_only_be_registered_once() {
    let cache = ProcessCache::new(pm_process::ProcReader::new(pm_netstate::ProcFs::default()));
    pm_process::register_default_tag_handlers(&cache).unwrap();
    assert!(pm_process::register_default_tag_handlers(&cache).is_err());
    assert_eq!(cache.tag_descriptions().len(), 11);
}
