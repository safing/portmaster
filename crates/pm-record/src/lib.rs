//! Compatible implementation of the Go `base/database/record` and
//! `base/database/accessor` packages.

pub mod accessor;
pub mod error;
mod key;
mod meta;
mod record;

pub use error::{Error, Result};
pub use key::Key;
pub use meta::Meta;
pub use record::Record;
