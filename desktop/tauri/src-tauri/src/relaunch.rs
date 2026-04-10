use std::{
    ffi::OsString,
    path::Path,
    process::{Command, Stdio},
    thread,
    time::Duration,
};

use log::{debug, error, warn};

const UI_RELAUNCH_HELPER_ENV: &str = "PORTMASTER_UI_RELAUNCH_HELPER";
const RELAUNCH_RETRY_COUNT: usize = 40;
const RELAUNCH_RETRY_DELAY: Duration = Duration::from_millis(500);

fn current_process_argv0() -> Option<OsString> {
    std::env::args_os().next()
}

fn is_usable_launch_program(program: &OsString) -> bool {
    let path = Path::new(program);

    // Fail closed for command-only values (for example, "portmaster"): we cannot
    // verify where they resolve to, so do not use them for relaunch.
    if !path.is_absolute() && path.components().count() <= 1 {
        return false;
    }

    if !path.exists() || !path.is_file() {
        return false;
    }

    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;

        if let Ok(meta) = std::fs::metadata(path) {
            return meta.permissions().mode() & 0o111 != 0;
        }
        return false;
    }

    #[cfg(not(unix))]
    {
        true
    }
}

fn resolve_launch_program() -> Result<OsString, String> {
    let current_exe = std::env::current_exe().ok().map(|p| p.into_os_string());
    let argv0 = current_process_argv0();

    if let Some(program) = current_exe.as_ref().filter(|p| is_usable_launch_program(p)) {
        return Ok(program.clone());
    }

    if let Some(program) = argv0.as_ref().filter(|p| is_usable_launch_program(p)) {
        return Ok(program.clone());
    }

    Err("failed to determine relaunch executable: no verified launchable file path from current_exe or argv0".to_string())
}

fn current_process_args() -> Vec<OsString> {
    std::env::args_os()
        .skip(1)
        .filter(|arg| {
            // On upgrade-triggered relaunch we always want to show the UI,
            // so do not propagate background startup flags.
            arg != "--background" && arg != "-b"
        })
        .collect()
}

pub fn request_ui_relaunch() -> Result<(), String> {
    let exe = resolve_launch_program()?;
    let args = current_process_args();

    let mut cmd = Command::new(&exe);
    cmd.args(&args)
        .env(UI_RELAUNCH_HELPER_ENV, "1")
        .stdin(Stdio::null())
        .stdout(Stdio::null())
        .stderr(Stdio::null());

    cmd.spawn()
        .map_err(|err| format!("failed to spawn relaunch helper process: {}", err))?;

    Ok(())
}

pub fn run_relaunch_helper_if_requested() {
    if std::env::var(UI_RELAUNCH_HELPER_ENV).ok().as_deref() != Some("1") {
        return;
    }

    if let Err(err) = run_relaunch_helper() {
        error!("[tauri] relaunch helper failed: {}", err);
    }

    std::process::exit(0);
}

fn run_relaunch_helper() -> Result<(), String> {
    let exe = resolve_launch_program()?;
    let args = current_process_args();

    debug!("[tauri] relaunch helper started");

    for attempt in 1..=RELAUNCH_RETRY_COUNT {
        let mut cmd = Command::new(&exe);
        cmd.args(&args)
            .env_remove(UI_RELAUNCH_HELPER_ENV)
            .stdin(Stdio::null())
            .stdout(Stdio::null())
            .stderr(Stdio::null());

        let mut child = cmd
            .spawn()
            .map_err(|err| format!("failed to spawn replacement process: {}", err))?;

        thread::sleep(RELAUNCH_RETRY_DELAY);

        match child.try_wait() {
            Ok(Some(status)) => {
                // Most commonly means the single-instance guard still detected a running instance.
                warn!(
                    "[tauri] replacement process exited quickly (attempt {}/{}; status={}), retrying",
                    attempt,
                    RELAUNCH_RETRY_COUNT,
                    status
                );
                thread::sleep(RELAUNCH_RETRY_DELAY);
            }
            Ok(None) => {
                debug!(
                    "[tauri] replacement process is running (attempt {}/{})",
                    attempt,
                    RELAUNCH_RETRY_COUNT
                );
                return Ok(());
            }
            Err(err) => {
                return Err(format!("failed to observe replacement process status: {}", err));
            }
        }
    }

    Err(format!(
        "failed to relaunch UI process after {} attempts",
        RELAUNCH_RETRY_COUNT
    ))
}
