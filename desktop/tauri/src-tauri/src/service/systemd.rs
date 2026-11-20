use log::{debug, error};

use super::status::StatusResult;
use super::{Result, ServiceManager, ServiceManagerError};
use std::os::unix::fs::PermissionsExt;
use std::{
    fs, io,
    process::{Command, ExitStatus, Stdio},
};

static SYSTEMCTL: &str = "systemctl";
static SERVICE_UNIT: &str = "portmaster.service";
// TODO(ppacher): add support for kdesudo and gksudo

enum SudoCommand {
    Pkexec,
    Gksu,
}

impl From<std::process::Output> for ServiceManagerError {
    fn from(output: std::process::Output) -> Self {
        let msg = String::from_utf8(output.stderr)
            .ok()
            .filter(|s| !s.trim().is_empty())
            .or_else(|| {
                String::from_utf8(output.stdout)
                    .ok()
                    .filter(|s| !s.trim().is_empty())
            })
            .unwrap_or_else(|| "Failed to run `systemctl`".to_string());

        ServiceManagerError::Other(output.status, msg)
    }
}

/// System Service manager implementation for Linux based distros.
pub struct SystemdServiceManager {}

impl SystemdServiceManager {
    /// Checks if systemctl is available in /sbin/ /bin, /usr/bin or /usr/sbin.
    ///
    /// Note that we explicitly check those paths to avoid returning true in case
    /// there's a systemctl binary in the cwd and PATH includes . since this may
    /// pose a security risk of running an untrusted binary with root privileges.
    pub fn is_installed() -> bool {
        let paths = vec![
            "/sbin/systemctl",
            "/bin/systemctl",
            "/usr/sbin/systemctl",
            "/usr/bin/systemctl",
        ];

        for path in paths {
            debug!("checking for systemctl at path {}", path);

            match fs::metadata(path) {
                Ok(md) => {
                    debug!("found systemctl at path {} ", path);

                    if md.is_file() && md.permissions().mode() & 0o111 != 0 {
                        return true;
                    }

                    error!(
                        "systemctl binary found but invalid permissions: {}",
                        md.permissions().mode()
                    );
                }
                Err(err) => {
                    // Most systems have systemctl in only one of the paths.
                    debug!("no systemctl binary at {}: {}", path, err);

                    continue;
                }
            };
        }

        error!("failed to find systemctl binary");

        false
    }
}

impl ServiceManager for SystemdServiceManager {
    fn status(&self) -> super::Result<StatusResult> {
        // `systemctl show` exits with 0 for units that do not exist, unlike
        // `is-active`, which prints "inactive" for them as well.
        let output = run(
            false,
            SYSTEMCTL,
            vec!["show", "-p", "LoadState", "-p", "ActiveState", SERVICE_UNIT],
        )?;
        if !output.status.success() {
            return Err(output.into());
        }

        let stdout = String::from_utf8(output.stdout)?;
        parse_show_status(&stdout)
            .ok_or_else(|| ServiceManagerError::Other(ExitStatus::default(), stdout))
    }

    fn start(&self) -> Result<StatusResult> {
        // This time we need to run as root through pkexec or similar binaries like kdesudo/gksudo.
        systemctl("start", SERVICE_UNIT, true)?;

        // Check the status again to be sure it's started now
        self.status()
    }
}

fn systemctl(
    cmd: &str,
    unit: &str,
    run_as_root: bool,
) -> std::result::Result<String, ServiceManagerError> {
    let output = run(run_as_root, SYSTEMCTL, vec![cmd, unit])?;

    // The command have been able to run (i.e. has been spawned and executed by the kernel).
    // We now need to check the exit code and "stdout/stderr" output in case of an error.
    if output.status.success() {
        Ok(String::from_utf8(output.stdout)?)
    } else {
        Err(output.into())
    }
}

fn run<'a>(root: bool, cmd: &'a str, args: Vec<&'a str>) -> std::io::Result<std::process::Output> {
    // clone the args vector so we can insert the actual command in case we're running
    // through pkexec or friends. This is just callled a couple of times on start-up
    // so cloning the vector does not add any mentionable performance impact here and it's better
    // than expecting a mutalble vector in the first place.

    let mut args = args.to_vec();

    let mut command = match root {
        true => {
            // if we run through pkexec and friends we need to append cmd as the second argument.

            args.insert(0, cmd);
            match get_sudo_cmd()? {
                SudoCommand::Pkexec => {
                    // disable the internal text-based prompt agent from pkexec because it won't work anyway.
                    args.insert(0, "--disable-internal-agent");
                    Command::new("/usr/bin/pkexec")
                }
                SudoCommand::Gksu => {
                    args.insert(0, "--message=Please enter your password:");
                    args.insert(1, "--sudo-mode");

                    Command::new("/usr/bin/gksudo")
                }
            }
        }
        false => Command::new(cmd),
    };

    command.env("LC_ALL", "C");

    command
        .stdin(Stdio::null())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped());

    command.args(args).output()
}

/// Maps the output of `systemctl show -p LoadState -p ActiveState` to a status.
/// Returns None if a property is missing.
fn parse_show_status(output: &str) -> Option<StatusResult> {
    let mut load_state = None;
    let mut active_state = None;
    for line in output.lines() {
        match line.trim().split_once('=') {
            Some(("LoadState", value)) => load_state = Some(value),
            Some(("ActiveState", value)) => active_state = Some(value),
            _ => {}
        }
    }

    if load_state? == "not-found" {
        return Some(StatusResult::NotFound);
    }
    match active_state? {
        "active" | "reloading" | "refreshing" => Some(StatusResult::Running),
        // inactive, failed, activating, deactivating, maintenance: the
        // service is installed but not (yet) serving.
        _ => Some(StatusResult::Stopped),
    }
}

fn get_sudo_cmd() -> std::result::Result<SudoCommand, std::io::Error> {
    if fs::metadata("/usr/bin/pkexec").is_ok() {
        return Ok(SudoCommand::Pkexec);
    }

    if fs::metadata("/usr/bin/gksudo").is_ok() {
        return Ok(SudoCommand::Gksu);
    }

    Err(std::io::Error::new(
        io::ErrorKind::NotFound,
        "failed to detect sudo command",
    ))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn show_status() {
        let cases = [
            ("LoadState=loaded\nActiveState=active\n", Some(StatusResult::Running)),
            ("ActiveState=reloading\nLoadState=loaded\n", Some(StatusResult::Running)),
            ("LoadState=loaded\nActiveState=inactive\n", Some(StatusResult::Stopped)),
            ("LoadState=loaded\nActiveState=failed\n", Some(StatusResult::Stopped)),
            ("LoadState=loaded\nActiveState=activating\n", Some(StatusResult::Stopped)),
            ("LoadState=not-found\nActiveState=inactive\n", Some(StatusResult::NotFound)),
            ("LoadState=masked\nActiveState=inactive\n", Some(StatusResult::Stopped)),
            ("LoadState=loaded\n", None),
            ("", None),
        ];
        for (output, want) in cases {
            assert_eq!(parse_show_status(output), want, "output: {output:?}");
        }
    }
}
