//! Port of Go `cmds/updatemgr/sign.go`: sign an update index with a jess
//! envelope from a directory trust store.

#[cfg(unix)]
use std::os::unix::fs::OpenOptionsExt;

use pm_jess::{add_json_signature, DirTrustStore};
use pm_updates::Index;

use crate::convert::now;

/// Help text for the `sign` subcommand.
pub const SIGN_HELP: &str = "Usage: updatemgr-rs sign [flags] <index.json file>

Sign an index with a jess envelope from a directory truststore.

Flags:
  --envelope NAME   required: envelope name in the trust store
  --tsdir DIR       truststore directory (default: JESS_TS_DIR / JESS_TSDIR)
  --tskeyring NS    not supported by this port (keyring truststore)
  --verbose, -v     enable verbose output
  --help, -h        show this help
";

/// Parsed arguments of the `sign` command.
#[derive(Debug, Default)]
pub struct SignOptions {
    pub envelope: Option<String>,
    pub tsdir: Option<String>,
    pub tskeyring: Option<String>,
    pub verbose: bool,
    pub index_file: Option<String>,
}

/// Parses flags like Go's cobra command (`--envelope`, `--tsdir`,
/// `--tskeyring`, `--verbose/-v` and one positional index file).
pub fn parse_sign_args(args: &mut std::vec::IntoIter<String>) -> Result<SignOptions, String> {
    let mut options = SignOptions::default();
    let mut positional = Vec::new();
    while let Some(arg) = args.next() {
        let mut value = |flag: &str| -> Result<String, String> {
            match args.next() {
                Some(value) if value.starts_with("--") => {
                    Err(format!("flag needs an argument: {flag}"))
                }
                Some(value) => Ok(value),
                None => Err(format!("flag needs an argument: {flag}")),
            }
        };
        match arg.as_str() {
            "--envelope" => options.envelope = Some(value("--envelope")?),
            "--tsdir" => options.tsdir = Some(value("--tsdir")?),
            "--tskeyring" => options.tskeyring = Some(value("--tskeyring")?),
            "--verbose" | "-v" => options.verbose = true,
            other if other.starts_with("--envelope=") => {
                options.envelope = Some(other["--envelope=".len()..].to_string());
            }
            other if other.starts_with("--tsdir=") => {
                options.tsdir = Some(other["--tsdir=".len()..].to_string());
            }
            other if other.starts_with("--tskeyring=") => {
                options.tskeyring = Some(other["--tskeyring=".len()..].to_string());
            }
            other if other.starts_with('-') => return Err(format!("unknown flag: {other}")),
            other => positional.push(other.to_string()),
        }
    }
    if positional.len() != 1 {
        return Err("sign expects exactly one index file".to_string());
    }
    options.index_file = positional.pop();
    Ok(options)
}

/// Environment variables used by Go's `setupTrustStore`.
fn env_first(names: &[&str]) -> Option<String> {
    names
        .iter()
        .find_map(|name| std::env::var(name).ok().filter(|value| !value.is_empty()))
}

/// Runs the command: validate the index, sign it, validate it again and
/// write it back with mode 0644.
pub fn sign(options: &SignOptions) -> Result<(), String> {
    let envelope_name = options
        .envelope
        .as_deref()
        .filter(|name| !name.is_empty())
        .ok_or_else(|| "required flag(s) \"envelope\" not set".to_string())?;
    let index_file = options
        .index_file
        .as_deref()
        .ok_or_else(|| "missing index file".to_string())?;

    let tsdir = options
        .tsdir
        .as_deref()
        .filter(|dir| !dir.is_empty())
        .map(str::to_string)
        .or_else(|| env_first(&["JESS_TS_DIR", "JESS_TSDIR"]));
    let tskeyring = options
        .tskeyring
        .as_deref()
        .filter(|ns| !ns.is_empty())
        .map(str::to_string)
        .or_else(|| env_first(&["JESS_TS_KEYRING", "JESS_TSKEYRING"]));

    // The keyring truststore needs the system keyring (libsecret / D-Bus),
    // which is deliberately not part of this Rust port. Directory is enough
    // for the release workflow; fail explicitly instead of silently skipping.
    if tsdir.is_none() && tskeyring.is_some() {
        return Err("keyring truststore is not supported by this port: use --tsdir or JESS_TS_DIR".to_string());
    }
    let tsdir = tsdir.ok_or_else(|| {
        "no truststore configured, please pass arguments or use env variables".to_string()
    })?;
    if options.verbose {
        println!("using truststore directory: {tsdir}");
    }
    let truststore = DirTrustStore::new(&tsdir).map_err(|e| e.to_string())?;
    let envelope = truststore
        .get_envelope(envelope_name)
        .map_err(|e| e.to_string())?;
    if options.verbose {
        println!(
            "using envelope {envelope_name} with suite {}",
            envelope.suite_id
        );
    }

    let unsigned = std::fs::read(index_file)
        .map_err(|error| format!("read index file: {error}"))?;
    validate_index(&unsigned, "invalid index")?;

    let signed = add_json_signature(&unsigned, &envelope, &truststore)
        .map_err(|error| format!("sign: {error}"))?;
    validate_index(&signed, "invalid index after signing")?;

    // Like Go's os.WriteFile: 0644 is the creation mode, an existing file
    // keeps its own permissions.
    let mut file = std::fs::OpenOptions::new()
        .write(true)
        .create(true)
        .truncate(true)
        .mode(0o644)
        .open(index_file)
        .map_err(|error| format!("write signed index file: {error}"))?;
    use std::io::Write;
    file.write_all(&signed)
        .map_err(|error| format!("write signed index file: {error}"))?;
    if options.verbose {
        println!("signed {index_file}");
    }
    Ok(())
}

fn validate_index(data: &[u8], context: &str) -> Result<(), String> {
    let index = Index::parse(data, "", None).map_err(|error| format!("{context}: {error}"))?;
    index
        .can_do_upgrades(now())
        .map_err(|error| format!("{context}: {error}"))
}
