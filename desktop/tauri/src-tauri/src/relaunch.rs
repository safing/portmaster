use std::{
    ffi::OsString,
    process::{Command, Stdio},
    thread,
    time::Duration,
};

use log::{debug, error, warn};

const UI_RELAUNCH_HELPER_ENV: &str = "PORTMASTER_UI_RELAUNCH_HELPER";
const RELAUNCH_RETRY_COUNT: usize = 40;
const RELAUNCH_RETRY_DELAY: Duration = Duration::from_millis(500);

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
    let exe = std::env::current_exe()
        .map_err(|err| format!("failed to get current executable path: {}", err))?;
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
    let exe = std::env::current_exe()
        .map_err(|err| format!("failed to get current executable path in relaunch helper: {}", err))?;
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
