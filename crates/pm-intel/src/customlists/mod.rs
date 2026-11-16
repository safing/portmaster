//! Port of the Go `service/intel/customlists` package.
//!
//! The custom filter list is a plain text file with one entry per line:
//! a domain, an IP address, a two-letter country code or an AS number
//! (`AS1234`). Everything after the first field is ignored, as are comments
//! starting with `#` and empty lines.
//!
//! # Compatibility notes
//!
//! * The file is parsed into an owned [`CustomLists`] instead of package-level
//!   maps; notifications and module states are emitted by the caller.
//! * Lines longer than 64 KiB fail the parse like Go's `bufio.Scanner`; invalid
//!   UTF-8 is replaced instead of being kept byte by byte.
//! * Domains keep the case they have in the file, exactly like Go's
//!   `dns.Fqdn`; the network module normalizes domains before lookup.
//! * The API endpoint, file watcher and module lifecycle are not ported yet.

mod config;
mod lists;

pub use config::{get_file_path, register_options, CFG_OPTION_CUSTOM_LIST_FILE_KEY};
pub use lists::{split_domain, CustomLists, ParseReport, INVALID_LINES_WARNING_RATIO};
