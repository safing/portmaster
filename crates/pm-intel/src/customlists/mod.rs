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
//! * The API endpoint and the module lifecycle (worker and config-change hook)
//!   are not ported yet; [`CustomListService::check_and_update`] is the unit
//!   both of them call.
//! * Lookups do not block while the file is parsed: the fresh entries are
//!   swapped in at the end instead of being written into the live maps.

mod config;
mod lists;
mod service;

pub use config::{get_file_path, register_options, CFG_OPTION_CUSTOM_LIST_FILE_KEY};
pub use lists::{split_domain, CustomLists, ParseReport, INVALID_LINES_WARNING_RATIO};
pub use service::{
    CustomListService, UpdateOutcome, PARSE_STATUS_NOTIFICATION_ID,
    PARSE_WARNING_NOTIFICATION_ID, ZERO_IP_NOTIFICATION_ID,
};
