use pm_record::Meta;

#[test]
fn update_sets_times_and_relative_expiry() {
    let mut m = Meta::default();
    m.update(1000);
    assert_eq!((m.created, m.modified, m.expires), (1000, 1000, 0));
    m.set_relative_expiry(60);
    assert_eq!(m.deleted, -60);
    m.update(2000);
    assert_eq!((m.created, m.modified, m.expires), (1000, 2000, 2060));
    assert!(!m.is_deleted());
}

#[test]
fn absolute_expiry_clears_relative() {
    let mut m = Meta {
        deleted: -5,
        ..Meta::default()
    };
    m.set_absolute_expiry(500);
    assert_eq!((m.expires, m.deleted), (500, 0));
    m.set_relative_expiry(-1);
    assert_eq!(m.deleted, 0);
}

#[test]
fn delete_reset_validity() {
    let mut m = Meta {
        expires: 100,
        secret: true,
        ..Meta::default()
    };
    assert!(m.check_validity(99));
    assert!(!m.check_validity(101));
    m.expires = 0;
    assert!(m.check_validity(i64::MAX));
    m.delete(7);
    assert!(m.is_deleted() && !m.check_validity(7));
    m.reset();
    assert_eq!((m.created, m.modified, m.expires, m.deleted), (0, 0, 0, 0));
    assert!(m.secret);
}

#[test]
fn permissions() {
    let mut m = Meta::default();
    assert!(m.check_permission(false, false));
    m.secret = true;
    assert!(!m.check_permission(true, false));
    assert!(m.check_permission(false, true));
    m.secret = false;
    m.cronjewel = true;
    assert!(!m.check_permission(false, true));
    assert!(m.check_permission(true, false));
}

#[test]
fn relative_expiry_getter() {
    let mut m = Meta::default();
    assert_eq!(m.get_relative_expiry(10), -1);
    m.expires = 30;
    assert_eq!(m.get_relative_expiry(10), 20);
    assert_eq!(m.get_relative_expiry(40), 0);
}

#[test]
fn update_does_not_overflow() {
    let mut m = Meta {
        deleted: i64::MIN,
        ..Meta::default()
    };
    m.update(i64::MAX);
}
