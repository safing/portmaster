//! Small manual test utility for the Windows kernel extension.
//!
//! Port of `cmds/winkext-test`: it installs/starts the driver, prints the
//! reported version and dumps the incoming info frames. It intentionally has
//! no opinions about firewall policy — it is a diagnostic tool, not part of
//! the core.

#[cfg(windows)]
fn main() {
    use pm_winkext::{severity_name, Kext};

    let args: Vec<String> = std::env::args().skip(1).collect();
    if args.len() != 1 {
        eprintln!("usage: winkext-test <path-to-portmaster-kext.sys>");
        std::process::exit(2);
    }

    let mut kext = Kext::new();
    kext.init(&args[0]);
    let version = match kext.start() {
        Ok(version) => version,
        Err(error) => {
            eprintln!("failed to start kext: {error}");
            std::process::exit(1);
        }
    };
    println!("kext version: {version}");

    let mut seen = 0_usize;
    loop {
        match kext.recv_info() {
            Ok(Some(info)) => {
                seen += 1;
                match info {
                    pm_winkext::Info::LogLine(line) => {
                        println!("[{}] {}", severity_name(line.severity), line.line);
                    }
                    other => println!("info: {other:?}"),
                }
            }
            Ok(None) => break,
            Err(error) => {
                eprintln!("failed to read kext info: {error}");
                break;
            }
        }
        if seen >= 1000 {
            println!("reached 1000 info frames, stopping");
            break;
        }
    }

    if let Err(error) = kext.stop() {
        eprintln!("failed to stop kext: {error}");
    }
}

#[cfg(not(windows))]
fn main() {
    eprintln!("winkext-test: this diagnostic utility is only available on Windows");
}
