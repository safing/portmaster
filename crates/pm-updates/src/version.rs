//! Lenient semantic versions, compatible with `hashicorp/go-version`.
//!
//! Go's update indexes use versions like `v1.2.3`, `1.2` and `1.2.3-rc1`.
//! Missing segments are padded to three (`1.2` → `1.2.0`), a leading `v` is
//! accepted and build metadata is ignored for comparisons.

use std::cmp::Ordering;
use std::fmt;

/// Errors of version parsing.
#[derive(Debug, thiserror::Error, PartialEq, Eq)]
pub enum VersionError {
    #[error("malformed version: {0}")]
    Malformed(String),
    #[error("invalid segment {0:?}")]
    InvalidSegment(String),
    #[error("invalid prerelease {0:?}")]
    InvalidPrerelease(String),
}

/// A prerelease identifier.
#[derive(Debug, Clone, PartialEq, Eq, PartialOrd, Ord)]
enum Identifier {
    Numeric(u64),
    Alpha(String),
}

/// A semantic version with the same parsing and comparison rules as Go.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Version {
    segments: Vec<u64>,
    prerelease: Vec<Identifier>,
    metadata: String,
}

impl Version {
    /// Parses a version string.
    pub fn parse(input: &str) -> Result<Version, VersionError> {
        let text = input
            .strip_prefix('v')
            .or_else(|| input.strip_prefix('V'))
            .unwrap_or(input);

        let (text, metadata) = match text.split_once('+') {
            Some((version, metadata)) => (version, metadata.to_string()),
            None => (text, String::new()),
        };
        if !metadata.is_empty() {
            validate_identifiers(&metadata)?;
        }

        let (core, prerelease) = match text.split_once('-') {
            Some((version, prerelease)) => (version, Some(prerelease)),
            None => (text, None),
        };
        if core.is_empty() {
            return Err(VersionError::Malformed(input.to_string()));
        }

        let mut segments = Vec::new();
        for segment in core.split('.') {
            let value: u64 = segment
                .parse()
                .map_err(|_| VersionError::InvalidSegment(segment.to_string()))?;
            segments.push(value);
        }
        while segments.len() < 3 {
            segments.push(0);
        }

        let mut prerelease_parts = Vec::new();
        if let Some(prerelease) = prerelease {
            if prerelease.is_empty() {
                return Err(VersionError::InvalidPrerelease(prerelease.to_string()));
            }
            for part in prerelease.split('.') {
                if part.is_empty() {
                    return Err(VersionError::InvalidPrerelease(prerelease.to_string()));
                }
                if let Ok(number) = part.parse::<u64>() {
                    prerelease_parts.push(Identifier::Numeric(number));
                } else {
                    validate_identifiers(part)?;
                    prerelease_parts.push(Identifier::Alpha(part.to_string()));
                }
            }
        }

        Ok(Version {
            segments,
            prerelease: prerelease_parts,
            metadata,
        })
    }

    /// Returns the numeric segments.
    pub fn segments(&self) -> &[u64] {
        &self.segments
    }

    /// Returns whether the version is a prerelease.
    pub fn is_prerelease(&self) -> bool {
        !self.prerelease.is_empty()
    }

    /// Returns whether the version is the zero version (`0.0.0`).
    pub fn is_zero(&self) -> bool {
        self.segments.iter().all(|segment| *segment == 0) && self.prerelease.is_empty()
    }
}

/// Validates prerelease and metadata identifiers.
fn validate_identifiers(text: &str) -> Result<(), VersionError> {
    for part in text.split('.') {
        if part.is_empty() || !part.chars().all(|c| c.is_ascii_alphanumeric() || c == '-') {
            return Err(VersionError::InvalidPrerelease(text.to_string()));
        }
    }
    Ok(())
}

impl Ord for Version {
    fn cmp(&self, other: &Version) -> Ordering {
        let length = self.segments.len().max(other.segments.len());
        for index in 0..length {
            let a = self.segments.get(index).copied().unwrap_or(0);
            let b = other.segments.get(index).copied().unwrap_or(0);
            match a.cmp(&b) {
                Ordering::Equal => {}
                ordering => return ordering,
            }
        }

        // A version without prerelease identifiers is greater.
        match (self.prerelease.is_empty(), other.prerelease.is_empty()) {
            (true, true) => Ordering::Equal,
            (true, false) => Ordering::Greater,
            (false, true) => Ordering::Less,
            (false, false) => self.prerelease.cmp(&other.prerelease),
        }
    }
}

impl PartialOrd for Version {
    fn partial_cmp(&self, other: &Version) -> Option<Ordering> {
        Some(self.cmp(other))
    }
}

impl fmt::Display for Version {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        let segments: Vec<String> = self.segments.iter().map(u64::to_string).collect();
        write!(f, "{}", segments.join("."))?;
        if !self.prerelease.is_empty() {
            let parts: Vec<String> = self
                .prerelease
                .iter()
                .map(|identifier| match identifier {
                    Identifier::Numeric(number) => number.to_string(),
                    Identifier::Alpha(text) => text.clone(),
                })
                .collect();
            write!(f, "-{}", parts.join("."))?;
        }
        if !self.metadata.is_empty() {
            write!(f, "+{}", self.metadata)?;
        }
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_and_normalizes() {
        let cases = [
            ("1.2.3", "1.2.3"),
            ("v1.2.3", "1.2.3"),
            ("1.2", "1.2.0"),
            ("1", "1.0.0"),
            ("1.2.3-rc1", "1.2.3-rc1"),
            ("1.2.3+build.5", "1.2.3+build.5"),
            ("1.2.3-rc1+build", "1.2.3-rc1+build"),
            ("1.2.3.4", "1.2.3.4"),
        ];
        for (input, expected) in cases {
            assert_eq!(Version::parse(input).unwrap().to_string(), expected);
        }

        for input in ["", "v", "1..3", "1.2.x", "-1.2.3"] {
            assert!(Version::parse(input).is_err(), "{input:?}");
        }
    }

    #[test]
    fn compares_like_semver() {
        let parse = |text| Version::parse(text).unwrap();
        assert_eq!(parse("1.2.3"), parse("1.2.3"));
        assert!(parse("1.2.3") < parse("1.2.4"));
        assert!(parse("1.2") == parse("1.2.0"));
        assert!(parse("1") == parse("1.0.0"));
        assert!(parse("1.2.3-rc1") < parse("1.2.3"));
        assert!(parse("1.2.3-rc1") < parse("1.2.3-rc2"));
        assert!(parse("1.2.3-alpha.1") < parse("1.2.3-alpha.2"));
        assert!(parse("2.0.0") > parse("1.9.9"));
        assert!(Version::parse("0.0.0").unwrap().is_zero());
    }
}
