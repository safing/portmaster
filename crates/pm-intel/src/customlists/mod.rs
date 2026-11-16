//! Port of the Go `service/intel/customlists` package.
//!
//! The custom filter list is a plain text file with one entry per line:
//! a domain, an IP address, a two-letter country code or an AS number
//! (`AS1234`). Everything after the first field is ignored, as are comments
//! starting with `#` and empty lines.

mod config;

pub use config::{
    get_file_path, register_options, CFG_OPTION_CUSTOM_LIST_FILE_KEY,
};
