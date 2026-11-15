use serde::{Deserialize, Serialize};

use crate::{Error, Result};

const GENCODE_SIZE: usize = 34;

/// Record metadata. `secret` and `cronjewel` are not part of the JSON form.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default, Serialize, Deserialize)]
#[serde(default)]
pub struct Meta {
    #[serde(rename = "Created")]
    pub created: i64,
    #[serde(rename = "Modified")]
    pub modified: i64,
    #[serde(rename = "Expires")]
    pub expires: i64,
    #[serde(rename = "Deleted")]
    pub deleted: i64,
    #[serde(skip)]
    pub secret: bool,
    #[serde(skip)]
    pub cronjewel: bool,
}

impl Meta {
    /// Gencode form without the leading format byte (34 bytes).
    pub fn gencode_marshal(&self) -> Vec<u8> {
        let mut b = Vec::with_capacity(GENCODE_SIZE);
        for v in [self.created, self.modified, self.expires, self.deleted] {
            b.extend_from_slice(&v.to_le_bytes());
        }
        b.push(u8::from(self.secret));
        b.push(u8::from(self.cronjewel));
        b
    }

    /// Parses the gencode form; trailing bytes are ignored like in Go.
    pub fn gencode_unmarshal(buf: &[u8]) -> Result<Meta> {
        if buf.len() < GENCODE_SIZE {
            return Err(Error::InsufficientData {
                got: buf.len(),
                want: GENCODE_SIZE,
            });
        }
        let int = |i: usize| {
            let mut a = [0u8; 8];
            a.copy_from_slice(&buf[i * 8..i * 8 + 8]);
            i64::from_le_bytes(a)
        };
        Ok(Meta {
            created: int(0),
            modified: int(1),
            expires: int(2),
            deleted: int(3),
            secret: buf[32] == 1,
            cronjewel: buf[33] == 1,
        })
    }

    pub fn set_absolute_expiry(&mut self, seconds: i64) {
        self.expires = seconds;
        self.deleted = 0;
    }

    /// Sets a TTL that is re-applied on every `update`. Negative values are ignored.
    pub fn set_relative_expiry(&mut self, seconds: i64) {
        if seconds >= 0 {
            self.deleted = -seconds;
        }
    }

    pub fn get_absolute_expiry(&self) -> i64 {
        self.expires
    }

    /// Seconds until expiry; negative means the record does not expire.
    pub fn get_relative_expiry(&self, now: i64) -> i64 {
        if self.expires == 0 {
            return -1;
        }
        self.expires.wrapping_sub(now).max(0)
    }

    pub fn make_crown_jewel(&mut self) {
        self.cronjewel = true;
    }

    pub fn is_crown_jewel(&self) -> bool {
        self.cronjewel
    }

    pub fn make_secret(&mut self) {
        self.secret = true;
    }

    pub fn is_secret(&self) -> bool {
        self.secret
    }

    pub fn update(&mut self, now: i64) {
        self.modified = now;
        if self.created == 0 {
            self.created = now;
        }
        if self.deleted < 0 {
            self.expires = now.wrapping_sub(self.deleted);
        }
    }

    /// Resets all metadata except the secret and crownjewel flags.
    pub fn reset(&mut self) {
        self.created = 0;
        self.modified = 0;
        self.expires = 0;
        self.deleted = 0;
    }

    pub fn delete(&mut self, now: i64) {
        self.deleted = now;
    }

    pub fn is_deleted(&self) -> bool {
        self.deleted > 0
    }

    pub fn check_validity(&self, now: i64) -> bool {
        if self.deleted > 0 {
            return false;
        }
        !(self.expires > 0 && self.expires < now)
    }

    pub fn check_permission(&self, local: bool, internal: bool) -> bool {
        (local || !self.cronjewel) && (internal || !self.secret)
    }
}
