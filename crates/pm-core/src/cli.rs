//! Command line options of the Rust core, mirroring `cmds/portmaster-core`.

use std::path::PathBuf;

/// The restart exit code, understood by portmaster-start (`service/instance.go`).
pub const RESTART_EXIT_CODE: i32 = 23;

/// Default listen address of the API.
pub const DEFAULT_API_ADDRESS: &str = "127.0.0.1:817";

/// Parsed command line options.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Options {
    pub bin_dir: PathBuf,
    pub data_dir: PathBuf,
    pub log_dir: PathBuf,
    pub log_to_stdout: bool,
    pub log_level: String,
    pub api_address: String,
    /// Default of `dns/listenAddress`; configuration is stronger.
    pub nameserver_address: String,
    pub dev_mode: bool,
    /// Binaries that may use the API without a key (`--allowed-clients`, repeatable).
    pub allowed_clients: Vec<PathBuf>,
    /// Write packet verdict metrics to this file (`--write-packet-metrics`).
    pub packet_metrics_file: Option<PathBuf>,
    pub print_version: bool,
    pub help: bool,
}

impl Default for Options {
    fn default() -> Self {
        Options {
            bin_dir: PathBuf::from("/usr/lib/portmaster"),
            data_dir: PathBuf::from("/var/lib/portmaster"),
            log_dir: PathBuf::from("/var/log/portmaster"),
            log_to_stdout: false,
            log_level: "info".to_string(),
            api_address: DEFAULT_API_ADDRESS.to_string(),
            nameserver_address: pm_nameserver::default_nameserver_address().to_string(),
            dev_mode: false,
            allowed_clients: Vec::new(),
            packet_metrics_file: None,
            print_version: false,
            help: false,
        }
    }
}

/// Parses command line arguments (without the program name).
pub fn parse(args: impl IntoIterator<Item = String>) -> Result<Options, String> {
    let mut options = Options::default();
    let mut args = args.into_iter().peekable();

    while let Some(arg) = args.next() {
        let (name, inline_value) = match arg.split_once('=') {
            Some((name, value)) => (name.to_string(), Some(value.to_string())),
            None => (arg.clone(), None),
        };

        let mut value = |name: &str| -> Result<String, String> {
            if let Some(value) = inline_value.clone() {
                return Ok(value);
            }
            args.next()
                .ok_or_else(|| format!("flag needs an argument: {name}"))
        };

        match name.as_str() {
            "--bin-dir" => options.bin_dir = PathBuf::from(value(&name)?),
            "--data-dir" => options.data_dir = PathBuf::from(value(&name)?),
            "--log-dir" => options.log_dir = PathBuf::from(value(&name)?),
            "--log" => options.log_level = value(&name)?,
            "--api-address" => options.api_address = value(&name)?,
            "--nameserver-address" => options.nameserver_address = value(&name)?,
            "--log-stdout" => options.log_to_stdout = true,
            "--devmode" => options.dev_mode = true,
            "--write-packet-metrics" => {
                options.packet_metrics_file = Some(PathBuf::from(value(&name)?))
            }
            "--allowed-clients" => options
                .allowed_clients
                .push(crate::api_auth::clean_path(&value(&name)?)),
            "--version" => options.print_version = true,
            "--help" | "-h" => options.help = true,
            other => return Err(format!("unknown flag: {other}")),
        }
    }

    Ok(options)
}

/// Returns the help text.
pub fn help() -> &'static str {
    "Usage: portmaster-core-rs [flags]

Flags:
      --bin-dir string       set directory for executable binaries (rw/ro)
      --data-dir string      set directory for variable data (rw)
      --log-stdout           log to stdout instead of a file
      --log-dir string       set directory for logs
      --log string           set log level to [trace|debug|info|warning|error|critical]
      --api-address string   set api listen address; configuration is stronger
      --nameserver-address string   set nameserver listen address; configuration is stronger
      --devmode              enable development mode
      --write-packet-metrics string   write packet verdict metrics to a file
      --allowed-clients      binary allowed to use the API (repeatable)
      --version              print version and exit
  -h, --help                 print this help and exit"
}

/// Returns the version string.
pub fn version() -> String {
    format!(
        "portmaster-core-rs {} (Rust core)",
        env!("CARGO_PKG_VERSION")
    )
}

#[cfg(test)]
mod tests {
    use super::*;

    fn args(list: &[&str]) -> Vec<String> {
        list.iter().map(|item| (*item).to_string()).collect()
    }

    #[test]
    fn defaults_match_linux_paths() {
        let options = parse(args(&[])).unwrap();
        assert_eq!(options.data_dir, PathBuf::from("/var/lib/portmaster"));
        assert_eq!(options.bin_dir, PathBuf::from("/usr/lib/portmaster"));
        assert_eq!(options.log_dir, PathBuf::from("/var/log/portmaster"));
        assert_eq!(options.api_address, "127.0.0.1:817");
        assert!(!options.dev_mode);
    }

    #[test]
    fn flags_accept_separate_and_inline_values() {
        let options = parse(args(&[
            "--data-dir",
            "/tmp/data",
            "--log-dir=/tmp/logs",
            "--api-address",
            "127.0.0.1:0",
            "--devmode",
            "--log-stdout",
            "--log",
            "debug",
        ]))
        .unwrap();
        assert_eq!(options.data_dir, PathBuf::from("/tmp/data"));
        assert_eq!(options.log_dir, PathBuf::from("/tmp/logs"));
        assert_eq!(options.api_address, "127.0.0.1:0");
        assert!(options.dev_mode);
        assert!(options.log_to_stdout);
        assert_eq!(options.log_level, "debug");
    }

    #[test]
    fn allowed_clients_is_repeatable_and_cleaned() {
        let options = parse(args(&[
            "--allowed-clients",
            "/opt/app//bin/../ui",
            "--allowed-clients=/usr/bin/tool",
        ]))
        .unwrap();
        assert_eq!(
            options.allowed_clients,
            vec![PathBuf::from("/opt/app/ui"), PathBuf::from("/usr/bin/tool")]
        );
    }

    #[test]
    fn packet_metrics_flag_is_parsed() {
        let options = parse(args(&[
            "--write-packet-metrics",
            "/tmp/pm-metrics.txt",
        ]))
        .unwrap();
        assert_eq!(
            options.packet_metrics_file,
            Some(PathBuf::from("/tmp/pm-metrics.txt"))
        );
        let inline = parse(args(&["--write-packet-metrics=/tmp/inline.txt"])).unwrap();
        assert_eq!(
            inline.packet_metrics_file,
            Some(PathBuf::from("/tmp/inline.txt"))
        );
        assert!(parse(args(&["--write-packet-metrics"])).is_err());
    }

    #[test]
    fn invalid_flags_are_rejected() {
        assert!(parse(args(&["--nope"])).is_err());
        assert!(parse(args(&["--data-dir"])).is_err());
    }
}
