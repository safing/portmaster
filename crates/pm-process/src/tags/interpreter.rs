//! Interpreter (shebang) tag handler.
//!
//! Port of `service/process/tags/interpreter_unix.go`. The handler recognizes
//! well-known interpreter binaries, extracts the script they are executing and
//! tags the process with the script path. The `CreateProfile` callback of the
//! Go implementation has no counterpart in the Rust [`TagHandler`] trait; the
//! tags produced here feed directly into `pm_profile::find_matching_profile`.

use std::path::{Path, PathBuf};
use std::sync::LazyLock;

use regex::Regex;

use crate::{Process, Tag, TagDescription, TagHandler};

/// A known interpreter, the script extensions it uses and the regex matching
/// its binary path.
struct InterpType {
    description: TagDescription,
    #[allow(dead_code)] // Extensions are used by Go's CreateProfile callback.
    extensions: &'static [&'static str],
    regex: Regex,
}

static KNOWN_INTERPRETERS: LazyLock<Vec<InterpType>> = LazyLock::new(|| {
    vec![
        InterpType {
            description: TagDescription {
                id: "python-script".to_string(),
                name: "Python Script".to_string(),
                description: String::new(),
            },
            extensions: &[".py", ".py2", ".py3"],
            regex: Regex::new(r"^(/usr)?/bin/python[23](\.[0-9]+)?$").unwrap(),
        },
        InterpType {
            description: TagDescription {
                id: "shell-script".to_string(),
                name: "Shell Script".to_string(),
                description: String::new(),
            },
            extensions: &[".sh", ".bash", ".ksh", ".zsh", ".ash"],
            regex: Regex::new(r"^(/usr)?/bin/(ba|k|z|a)?sh$").unwrap(),
        },
        InterpType {
            description: TagDescription {
                id: "perl-script".to_string(),
                name: "Perl Script".to_string(),
                description: String::new(),
            },
            extensions: &[".pl"],
            regex: Regex::new(r"^(/usr)?/bin/perl$").unwrap(),
        },
        InterpType {
            description: TagDescription {
                id: "ruby-script".to_string(),
                name: "Ruby Script".to_string(),
                description: String::new(),
            },
            extensions: &[".rb"],
            regex: Regex::new(r"^(/usr)?/bin/ruby$").unwrap(),
        },
        InterpType {
            description: TagDescription {
                id: "nodejs-script".to_string(),
                name: "NodeJS Script".to_string(),
                description: String::new(),
            },
            extensions: &[".js"],
            regex: Regex::new(r"^(/usr)?/bin/node(js)?$").unwrap(),
        },
    ]
});

/// Tags processes that run a script through a known interpreter.
pub struct InterpHandler;

impl TagHandler for InterpHandler {
    fn name(&self) -> &str {
        "Interpreter"
    }

    fn tag_descriptions(&self) -> Vec<TagDescription> {
        KNOWN_INTERPRETERS
            .iter()
            .map(|it| it.description.clone())
            .collect()
    }

    fn add_tags(&self, process: &mut Process) {
        // Match the process path against the known interpreter binaries.
        let Some(matched) = KNOWN_INTERPRETERS
            .iter()
            .find(|it| it.regex.is_match(&process.path))
        else {
            return;
        };

        // Split the command line like Go's shlex.Split.
        let Some(args) = shlex::split(&process.cmd_line) else {
            return;
        };
        let Some(arg0) = args.first() else {
            return;
        };

        if matched.regex.is_match(arg0) {
            // The interpreter itself is argv[0]; the script is the next arg.
            if args.len() == 1 {
                // Interactive interpreter session: no script to tag.
                return;
            }
            let script = &args[1];
            let script_path = if Path::new(script).is_absolute() {
                PathBuf::from(script)
            } else {
                PathBuf::from(clean_path(&format!("{}/{}", process.cwd, script)))
            };

            // The script must be a readable UTF-8 file.
            if !file_must_be_utf8(&script_path) {
                return;
            }

            let value = script_path.to_string_lossy().into_owned();
            process.tags.push(Tag {
                key: matched.description.id.clone(),
                value: value.clone(),
            });
            process.matching_path = value;
            return;
        }

        // argv[0] is not the interpreter path itself; tag it directly.
        process.tags.push(Tag {
            key: matched.description.id.clone(),
            value: arg0.clone(),
        });
        process.matching_path = arg0.clone();
    }
}

/// Port of Go's `fileMustBeUTF8`: read at most 128 bytes and ensure every rune
/// is valid UTF-8. Go treats `utf8.RuneError` (including a valid U+FFFD) as a
/// failure, which is reproduced by rejecting U+FFFD explicitly.
fn file_must_be_utf8(path: &Path) -> bool {
    use std::io::Read;

    let Ok(mut file) = std::fs::File::open(path) else {
        return false;
    };

    let mut buf = [0u8; 128];
    let mut filled = 0usize;
    while filled < buf.len() {
        match file.read(&mut buf[filled..]) {
            Ok(0) => break,
            Ok(count) => filled += count,
            Err(_) => break,
        }
    }
    if filled == 0 {
        return false;
    }

    match std::str::from_utf8(&buf[..filled]) {
        Ok(text) => !text.contains('\u{FFFD}'),
        Err(_) => false,
    }
}

/// Lexically clean a Unix path like Go's `filepath.Clean`.
fn clean_path(path: &str) -> String {
    if path.is_empty() {
        return ".".to_string();
    }
    let rooted = path.starts_with('/');
    let mut parts: Vec<&str> = Vec::new();
    for part in path.split('/') {
        match part {
            "" | "." => {}
            ".." => {
                if parts.last().is_some_and(|last| *last != "..") {
                    parts.pop();
                } else if !rooted {
                    parts.push("..");
                }
            }
            other => parts.push(other),
        }
    }
    let mut result = String::new();
    if rooted {
        result.push('/');
    }
    result.push_str(&parts.join("/"));
    if result.is_empty() {
        result.push('.');
    }
    result
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn cleans_paths_like_go() {
        assert_eq!(clean_path("/home/test/./script.py"), "/home/test/script.py");
        assert_eq!(clean_path("/home/test/../other/script.py"), "/home/other/script.py");
        assert_eq!(clean_path("/a//b///c"), "/a/b/c");
        assert_eq!(clean_path("../a/./b"), "../a/b");
        assert_eq!(clean_path(""), ".");
    }
}
