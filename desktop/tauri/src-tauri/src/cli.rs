use log::LevelFilter;

// #[cfg(not(debug_assertions))]
// const DEFAULT_LOG_LEVEL: log::LevelFilter = log::LevelFilter::Warn;

// #[cfg(debug_assertions)]
const DEFAULT_LOG_LEVEL: log::LevelFilter = log::LevelFilter::Debug;

#[derive(Debug)]
pub struct CliArguments {
    // Path to the installation directory
    pub data: Option<String>,

    // Log level to use: off, error, warn, info, debug, trace
    pub log_level: log::LevelFilter,

    // Start in the background without opening a window
    pub background: bool,

    // Enable experimental notifications via Tauri. Replaces the notifier app.
    pub with_prompts: bool,

    // Enable experimental prompt support via Tauri. Replaces the notifier app.
    pub with_notifications: bool,
}

impl CliArguments {
    fn parse_log(&mut self, level: String) {
        self.log_level = match level.as_ref() {
            "off" => LevelFilter::Off,
            "error" => LevelFilter::Error,
            "warn" => LevelFilter::Warn,
            "info" => LevelFilter::Info,
            "debug" => LevelFilter::Debug,
            "trace" => LevelFilter::Trace,
            _ => DEFAULT_LOG_LEVEL,
        }
    }
}

pub fn parse(raw: impl IntoIterator<Item = impl Into<std::ffi::OsString>>) -> CliArguments {
    let mut cli = CliArguments {
        data: None,
        log_level: DEFAULT_LOG_LEVEL,
        background: false,
        with_prompts: true,
        with_notifications: true,
    };

    let raw = clap_lex::RawArgs::new(raw);
    let mut cursor = raw.cursor();
    raw.next(&mut cursor); // Skip the bin

    while let Some(arg) = raw.next(&mut cursor) {
        if let Some((long, value)) = arg.to_long() {
            match long {
                Ok("data") => {
                    if let Some(value) = value {
                        cli.data = Some(value.to_string_lossy().into_owned());
                    }
                }
                Ok("log") => {
                    if let Some(value) = value {
                        cli.parse_log(value.to_string_lossy().into_owned());
                    }
                }
                Ok("background") => {
                    cli.background = true;
                }
                Ok("no-prompts") => {
                    cli.with_prompts = false;
                }
                Ok("no-notifications") => {
                    cli.with_notifications = false;
                }
                _ => {
                    // Ignore unexpected flags
                }
            }
        } else if let Some(mut shorts) = arg.to_short() {
            while let Some(short) = shorts.next() {
                match short {
                    Ok('l') => {
                        if let Some(value) = shorts.next_value_os() {
                            let mut str = value.to_string_lossy().into_owned();
                            _ = str.remove(0); // remove first "=" from value (in -l=warn value will be "=warn")
                            cli.parse_log(str);
                        }
                    }
                    Ok('d') => {
                        if let Some(value) = shorts.next_value_os() {
                            let mut str = value.to_string_lossy().into_owned();
                            _ = str.remove(0); // remove first "=" from value (in -d=/data value will be "=/data")
                            cli.data = Some(str);
                        }
                    }
                    Ok('b') => cli.background = true,
                    _ => {
                        // Ignore unexpected flags
                    }
                }
            }
        }
    }

    cli
}
