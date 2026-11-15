use std::fmt;

/// A database key split into database name and key part.
#[derive(Debug, Clone, PartialEq, Eq, Hash, Default)]
pub struct Key {
    pub db: String,
    pub key: String,
}

impl Key {
    /// Splits at the first `:`. Without a colon the whole string becomes the
    /// database name, like Go `ParseKey`.
    pub fn parse(s: &str) -> Key {
        match s.split_once(':') {
            Some((db, key)) => Key {
                db: db.into(),
                key: key.into(),
            },
            None => Key {
                db: s.into(),
                key: String::new(),
            },
        }
    }

    pub fn new(db: &str, key: &str) -> Key {
        Key {
            db: db.into(),
            key: key.into(),
        }
    }
}

impl fmt::Display for Key {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "{}:{}", self.db, self.key)
    }
}
