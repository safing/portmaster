//! Linux WireGuard kill-switch bypass hook — port of Go
//! `service/interop/ivpn/hook_linux.go`.
//!
//! When IVPN connects over WireGuard, `wg-quick` installs a raw kill-switch that
//! drops reverse-NAT replies arriving on loopback with a non-local source. This
//! module installs a narrow exception rule (nftables, or iptables as a fallback)
//! only while a WireGuard connection is active *and* SPN or split-tunneling is
//! enabled.
//!
//! All privileged I/O is behind the injectable [`CommandRunner`] trait. With a
//! fake runner the lifecycle is testable without root; with the real
//! [`SystemCommandRunner`] a missing binary, missing table or permission error
//! is reported as a `Result::Err`/no-op and logged as a warning by the engine —
//! the process never panics or dies because the hook could not be applied.

use std::net::IpAddr;
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicI64, Ordering};
use std::sync::{Arc, Mutex};

use serde::Deserialize;

/// nft table created by IVPN's wg-quick setup (tied to the `wgivpn` interface).
pub const NFT_TABLE: &str = "wg-quick-wgivpn";
/// Comment marking rules managed by Portmaster.
pub const BYPASS_COMMENT: &str = "portmaster-wg-ks-bypass";
/// Chain in the `ip` family where the kill-switch drop lives.
pub const NFT_CHAIN: &str = "preraw";
const NFT: &str = "nft";
const IPTABLES: &str = "iptables";

/// Output of a successfully executed command.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CommandOutput {
    pub stdout: Vec<u8>,
}

/// Injectable command runner (the `nft`/`iptables` "adapter").
///
/// `exists` is the equivalent of Go's `exec.LookPath`; `run` returns `Err` when
/// the program is missing, not executable or exits with a non-zero status.
pub trait CommandRunner: Send + Sync {
    /// Returns whether `program` can be found in `PATH` (or at its literal path).
    fn exists(&self, program: &str) -> bool;
    /// Runs `program` with `args` and returns its stdout on success.
    fn run(&self, program: &str, args: &[&str]) -> Result<CommandOutput, String>;
}

#[cfg(unix)]
fn is_executable(path: &Path) -> bool {
    use std::os::unix::fs::PermissionsExt;
    std::fs::metadata(path)
        .map(|metadata| metadata.permissions().mode() & 0o111 != 0)
        .unwrap_or(false)
}

#[cfg(not(unix))]
fn is_executable(_path: &Path) -> bool {
    true
}

/// Production [`CommandRunner`] using `std::process::Command` and `PATH` lookup.
pub struct SystemCommandRunner;

impl CommandRunner for SystemCommandRunner {
    fn exists(&self, program: &str) -> bool {
        let candidate = Path::new(program);
        if candidate.components().count() > 1 {
            return candidate.is_file() && is_executable(candidate);
        }
        std::env::var_os("PATH").is_some_and(|paths| {
            std::env::split_paths(&paths).any(|directory: PathBuf| {
                let candidate = directory.join(program);
                candidate.is_file() && is_executable(&candidate)
            })
        })
    }

    fn run(&self, program: &str, args: &[&str]) -> Result<CommandOutput, String> {
        let output = std::process::Command::new(program)
            .args(args)
            .output()
            .map_err(|error| format!("failed to run {program}: {error}"))?;
        if !output.status.success() {
            let detail = String::from_utf8_lossy(&output.stderr).trim().to_string();
            return Err(if detail.is_empty() {
                format!("{program} exited with {}", output.status)
            } else {
                format!("{program} exited with {}: {detail}", output.status)
            });
        }
        Ok(CommandOutput {
            stdout: output.stdout,
        })
    }
}

#[derive(Debug, Deserialize)]
struct NftRule {
    handle: i64,
}

#[derive(Debug, Deserialize)]
struct NftInsert {
    rule: NftRule,
}

#[derive(Debug, Deserialize)]
struct NftEntry {
    insert: Option<NftInsert>,
}

#[derive(Debug, Deserialize)]
struct NftOutput {
    #[serde(default)]
    nftables: Vec<NftEntry>,
}

/// Extracts the rule handle from the JSON output of
/// `nft --echo --json insert rule ...`, matching Go's `parseNftInsertHandle`.
pub fn parse_nft_insert_handle(data: &[u8]) -> Result<i64, String> {
    let output: NftOutput =
        serde_json::from_slice(data).map_err(|error| format!("invalid nft output: {error}"))?;
    for entry in output.nftables {
        if let Some(insert) = entry.insert {
            if insert.rule.handle <= 0 {
                return Err(format!("invalid nft rule handle: {}", insert.rule.handle));
            }
            return Ok(insert.rule.handle);
        }
    }
    Err("no rule entry found in nft output".to_string())
}

/// Inputs the hook needs to decide whether a compatibility rule is wanted.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct HookConfig {
    /// Value of `spn/enable`.
    pub spn_enabled: bool,
    /// Value of `splittun/enable`.
    pub splittun_enabled: bool,
    /// Local IPv4/IPv6 address reported by a connected WireGuard session.
    pub wireguard_local_ip: Option<String>,
}

/// Manages the lifecycle of the WireGuard kill-switch bypass rule.
pub struct NftHook {
    runner: Arc<dyn CommandRunner>,
    /// Handle of the nft rule we installed (0 = none).
    nft_handle: AtomicI64,
    /// Local IP of the iptables fallback rule we installed (None = none).
    iptables_ip: Mutex<Option<String>>,
}

impl Default for NftHook {
    fn default() -> Self {
        Self::new(Arc::new(SystemCommandRunner))
    }
}

impl NftHook {
    /// Creates a hook with an injected command runner (for tests).
    pub fn new(runner: Arc<dyn CommandRunner>) -> Self {
        NftHook {
            runner,
            nft_handle: AtomicI64::new(0),
            iptables_ip: Mutex::new(None),
        }
    }

    /// Reconciles the rule with the current WireGuard/SPN state.
    ///
    /// Previously managed rules are always removed first, which makes the
    /// operation idempotent across reconnects, IP changes and config toggles.
    /// Returns an error when a command fails; the caller logs it as a warning.
    pub fn reconcile(&self, config: &HookConfig) -> Result<(), String> {
        let has_nft = self.runner.exists(NFT);
        let has_iptables = self.runner.exists(IPTABLES);
        if !has_nft && !has_iptables {
            // Graceful degradation: no firewall tools available.
            return Ok(());
        }

        // Always clean previously managed rules first, like Go's reconcile.
        self.remove_nft_rule(has_nft);
        self.remove_iptables_rule(has_iptables);

        let Some(ip_text) = config.wireguard_local_ip.as_deref() else {
            return Ok(());
        };
        if !config.spn_enabled && !config.splittun_enabled {
            return Ok(());
        }
        let address: IpAddr = match ip_text.parse() {
            Ok(address) => address,
            Err(_) => return Ok(()),
        };
        let wg_ip = address.to_string();

        if has_nft && self.nft_table_exists() {
            let args = [
                "--echo",
                "--json",
                "insert",
                "rule",
                "ip",
                NFT_TABLE,
                NFT_CHAIN,
                "iifname",
                "lo",
                "ip",
                "daddr",
                &wg_ip,
                "fib",
                "saddr",
                "type",
                "!=",
                "local",
                "accept",
                "comment",
                BYPASS_COMMENT,
            ];
            let output = self
                .runner
                .run(NFT, &args)
                .map_err(|error| format!("failed to insert nft rule: {error}"))?;
            let handle = parse_nft_insert_handle(&output.stdout)
                .map_err(|error| format!("failed to parse nft rule handle: {error}"))?;
            self.nft_handle.store(handle, Ordering::SeqCst);
            tracing::debug!(
                handle,
                address = %wg_ip,
                "IVPN: Inserted nft SPN compatibility rule for WireGuard"
            );
            return Ok(());
        }

        if has_iptables {
            let cidr = format!("{wg_ip}/32");
            let args = [
                "-t", "raw", "-I", "PREROUTING", "1", "-d", &cidr, "-i", "lo", "-m",
                "addrtype", "!", "--src-type", "LOCAL", "-m", "comment", "--comment",
                BYPASS_COMMENT, "-j", "ACCEPT",
            ];
            self.runner
                .run(IPTABLES, &args)
                .map_err(|error| format!("failed to insert iptables fallback rule: {error}"))?;
            *self.iptables_ip.lock().expect("lock poisoned") = Some(wg_ip.clone());
            tracing::debug!(
                address = %wg_ip,
                "IVPN: Inserted iptables SPN compatibility rule for WireGuard"
            );
        }
        Ok(())
    }

    fn nft_table_exists(&self) -> bool {
        self.runner
            .run(NFT, &["list", "table", "ip", NFT_TABLE])
            .is_ok()
    }

    fn remove_nft_rule(&self, has_nft: bool) {
        let handle = self.nft_handle.swap(0, Ordering::SeqCst);
        if !has_nft || handle == 0 {
            return;
        }
        let handle_text = handle.to_string();
        if let Err(error) = self.runner.run(
            NFT,
            &["delete", "rule", "ip", NFT_TABLE, NFT_CHAIN, "handle", &handle_text],
        ) {
            tracing::warn!(%error, "IVPN: failed to delete managed nft rule");
        }
    }

    fn remove_iptables_rule(&self, has_iptables: bool) {
        let ip = self.iptables_ip.lock().expect("lock poisoned").take();
        let Some(ip) = ip else {
            return;
        };
        if !has_iptables {
            return;
        }
        let cidr = format!("{ip}/32");
        let args = [
            "-t", "raw", "-D", "PREROUTING", "-d", &cidr, "-i", "lo", "-m", "addrtype", "!",
            "--src-type", "LOCAL", "-m", "comment", "--comment", BYPASS_COMMENT, "-j", "ACCEPT",
        ];
        if let Err(error) = self.runner.run(IPTABLES, &args) {
            tracing::warn!(%error, "IVPN: failed to delete managed iptables rule");
        }
    }

    /// Handle of the currently installed nft rule, for diagnostics/tests.
    pub fn installed_handle(&self) -> i64 {
        self.nft_handle.load(Ordering::SeqCst)
    }

    /// IP of the currently installed iptables fallback rule, for diagnostics/tests.
    pub fn installed_iptables_ip(&self) -> Option<String> {
        self.iptables_ip.lock().expect("lock poisoned").clone()
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::collections::HashSet;

    const INSERT_JSON: &str = r#"{"nftables":[{"insert":{"rule":{"family":"ip","table":"wg-quick-wgivpn","chain":"preraw","handle":77}}}]}"#;

    #[derive(Default)]
    struct FakeRunner {
        available: HashSet<String>,
        log: Mutex<Vec<Vec<String>>>,
        fail_first_arg: Mutex<Option<String>>,
        insert_output: Mutex<String>,
    }

    impl FakeRunner {
        fn with_tools(tools: &[&str]) -> Arc<FakeRunner> {
            Arc::new(FakeRunner {
                available: tools.iter().map(|tool| (*tool).to_string()).collect(),
                insert_output: Mutex::new(INSERT_JSON.to_string()),
                ..FakeRunner::default()
            })
        }

        fn commands(&self) -> Vec<Vec<String>> {
            self.log.lock().expect("lock poisoned").clone()
        }

        fn fail_on(&self, first_arg: &str) {
            *self.fail_first_arg.lock().expect("lock poisoned") = Some(first_arg.to_string());
        }
    }

    impl CommandRunner for FakeRunner {
        fn exists(&self, program: &str) -> bool {
            self.available.contains(program)
        }

        fn run(&self, program: &str, args: &[&str]) -> Result<CommandOutput, String> {
            let command: Vec<String> = std::iter::once(program.to_string())
                .chain(args.iter().map(|argument| (*argument).to_string()))
                .collect();
            self.log.lock().expect("lock poisoned").push(command);
            if self
                .fail_first_arg
                .lock()
                .expect("lock poisoned")
                .as_deref()
                == args.first().copied()
            {
                return Err(format!("{program}: permission denied"));
            }
            let stdout = if program == NFT && args.contains(&"insert") {
                self.insert_output.lock().expect("lock poisoned").as_bytes().to_vec()
            } else {
                Vec::new()
            };
            Ok(CommandOutput { stdout })
        }
    }

    fn wg_config(spn: bool, split: bool, ip: &str) -> HookConfig {
        HookConfig {
            spn_enabled: spn,
            splittun_enabled: split,
            wireguard_local_ip: Some(ip.to_string()),
        }
    }

    #[test]
    fn parses_nft_insert_handle() {
        assert_eq!(parse_nft_insert_handle(INSERT_JSON.as_bytes()).unwrap(), 77);
        assert!(parse_nft_insert_handle(br#"{"nftables":[]}"#).is_err());
        assert!(parse_nft_insert_handle(b"not json").is_err());
    }

    #[test]
    fn no_tools_is_a_noop() {
        let runner = FakeRunner::with_tools(&[]);
        let hook = NftHook::new(runner.clone());
        hook.reconcile(&wg_config(true, false, "10.0.0.2")).unwrap();
        assert!(runner.commands().is_empty());
    }

    #[test]
    fn idempotent_nft_lifecycle() {
        let runner = FakeRunner::with_tools(&[NFT]);
        let hook = NftHook::new(runner.clone());
        let config = wg_config(true, false, "10.0.0.2");

        hook.reconcile(&config).unwrap();
        assert_eq!(hook.installed_handle(), 77);
        hook.reconcile(&config).unwrap();
        assert_eq!(hook.installed_handle(), 77);

        // Each reconcile removes the previous rule before inserting a new one.
        let commands = runner.commands();
        let deletes = commands
            .iter()
            .filter(|command| command.get(1).map(String::as_str) == Some("delete"))
            .count();
        let inserts = commands
            .iter()
            .filter(|command| command.get(3).map(String::as_str) == Some("insert"))
            .count();
        assert_eq!(deletes, 1, "first reconcile has nothing to delete");
        assert_eq!(inserts, 2);
        assert!(commands.iter().any(|command| {
            command.first().map(String::as_str) == Some(NFT)
                && command.iter().any(|part| part == "portmaster-wg-ks-bypass")
        }));
    }

    #[test]
    fn disabling_features_removes_the_rule() {
        let runner = FakeRunner::with_tools(&[NFT]);
        let hook = NftHook::new(runner.clone());
        hook.reconcile(&wg_config(false, true, "10.0.0.2")).unwrap();
        assert_eq!(hook.installed_handle(), 77);
        hook.reconcile(&HookConfig::default()).unwrap();
        assert_eq!(hook.installed_handle(), 0);
        assert!(runner.commands().iter().any(|command| {
            command.get(1).map(String::as_str) == Some("delete")
        }));
    }

    #[test]
    fn iptables_fallback_without_nft() {
        let runner = FakeRunner::with_tools(&[IPTABLES]);
        let hook = NftHook::new(runner.clone());
        hook.reconcile(&wg_config(false, true, "10.0.0.2")).unwrap();
        assert_eq!(hook.installed_iptables_ip().as_deref(), Some("10.0.0.2"));
        let commands = runner.commands();
        assert!(commands.iter().any(|command| command.first().map(String::as_str) == Some(IPTABLES)));
        assert!(!commands.iter().any(|command| command.first().map(String::as_str) == Some(NFT)));
    }

    #[test]
    fn invalid_ip_and_no_features_do_nothing() {
        let runner = FakeRunner::with_tools(&[NFT]);
        let hook = NftHook::new(runner.clone());
        hook.reconcile(&wg_config(true, false, "not-an-ip")).unwrap();
        hook.reconcile(&HookConfig {
            wireguard_local_ip: Some("10.0.0.2".to_string()),
            ..HookConfig::default()
        })
        .unwrap();
        assert_eq!(hook.installed_handle(), 0);
        // No feature enabled and an invalid IP: no command is run at all.
        assert!(runner.commands().is_empty());
    }

    #[test]
    fn command_failure_is_reported_without_panicking() {
        let runner = FakeRunner::with_tools(&[NFT]);
        // The nft insert command starts with `--echo`.
        runner.fail_on("--echo");
        let hook = NftHook::new(runner);
        let error = hook
            .reconcile(&wg_config(true, false, "10.0.0.2"))
            .unwrap_err();
        assert!(error.contains("failed to insert nft rule"), "{error}");
        assert_eq!(hook.installed_handle(), 0);
    }
}
