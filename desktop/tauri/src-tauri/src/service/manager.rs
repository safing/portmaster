use std::process::{Command, ExitStatus, Stdio};
use std::{fs, io};

use thiserror::Error;

#[cfg(target_os = "linux")]
use std::os::unix::fs::PermissionsExt;

use super::status::StatusResult;

static SYSTEMCTL: &str = "systemctl";
// TODO(ppacher): add support for kdesudo and gksudo

enum SudoCommand {
    Pkexec,
    Gksu,
}
