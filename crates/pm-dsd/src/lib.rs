//! Compatible implementation of the Go `structures/{varint,dsd}` packages.

pub mod error;
pub mod format;
pub mod gojson;
pub mod list;
pub mod varint;

mod dsd;

pub use dsd::{dump, load};
pub use error::{Error, Result};
