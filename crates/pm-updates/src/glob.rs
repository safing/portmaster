//! Minimal glob matching with Go `gobwas/glob` semantics.
//!
//! Go compiles the scan patterns with the path separator as the only separator,
//! so `*` and `?` never cross a `/` while `**` does. This is a port of the
//! subset the update scanner uses; alternation (`{a,b}`) is rejected at compile
//! time instead of being silently misinterpreted.

/// An error of glob compilation.
#[derive(Debug, thiserror::Error, PartialEq, Eq)]
pub enum GlobError {
    #[error("unclosed character class")]
    UnclosedClass,
    #[error("unsupported pattern syntax at {0:?}")]
    Unsupported(String),
    #[error("trailing escape")]
    TrailingEscape,
}

#[derive(Debug, Clone, PartialEq, Eq)]
enum Token {
    /// A literal character.
    Literal(char),
    /// `?`: one character except the separator.
    AnyChar,
    /// `*`: any number of characters except the separator.
    AnySequence,
    /// `**`: any number of characters, including separators.
    AnySequenceDeep,
    /// `[...]`: one character from the class.
    Class {
        negated: bool,
        ranges: Vec<(char, char)>,
    },
}

/// A compiled glob pattern.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Glob {
    tokens: Vec<Token>,
}

impl Glob {
    /// Compiles a pattern with `/` as the path separator.
    pub fn compile(pattern: &str) -> Result<Glob, GlobError> {
        let chars: Vec<char> = pattern.chars().collect();
        let mut tokens = Vec::with_capacity(chars.len());
        let mut index = 0;

        while index < chars.len() {
            match chars[index] {
                '\\' => {
                    let Some(&escaped) = chars.get(index + 1) else {
                        return Err(GlobError::TrailingEscape);
                    };
                    tokens.push(Token::Literal(escaped));
                    index += 2;
                }
                '*' => {
                    if chars.get(index + 1) == Some(&'*') {
                        // Collapse runs of more than two stars.
                        while chars.get(index + 1) == Some(&'*') {
                            index += 1;
                        }
                        tokens.push(Token::AnySequenceDeep);
                    } else {
                        tokens.push(Token::AnySequence);
                    }
                    index += 1;
                }
                '?' => {
                    tokens.push(Token::AnyChar);
                    index += 1;
                }
                '[' => {
                    let (token, next) = compile_class(&chars, index)?;
                    tokens.push(token);
                    index = next;
                }
                '{' => {
                    return Err(GlobError::Unsupported("{".to_string()));
                }
                literal => {
                    tokens.push(Token::Literal(literal));
                    index += 1;
                }
            }
        }

        Ok(Glob { tokens })
    }

    /// Returns whether the path matches the pattern.
    pub fn is_match(&self, path: &str) -> bool {
        let chars: Vec<char> = path.chars().collect();
        match_from(&self.tokens, &chars, 0, 0)
    }
}

/// Compiles a character class starting at `start` (the `[`).
///
/// Returns the token and the index after the closing `]`.
fn compile_class(chars: &[char], start: usize) -> Result<(Token, usize), GlobError> {
    let mut index = start + 1;
    let mut negated = false;

    // gobwas only knows `[!...]`; a leading `^` is a literal member.
    if chars.get(index) == Some(&'!') {
        negated = true;
        index += 1;
    }

    let mut ranges = Vec::new();
    let mut first = true;

    loop {
        let Some(&char) = chars.get(index) else {
            return Err(GlobError::UnclosedClass);
        };

        // A `]` closes the class unless it is the first member.
        if char == ']' && !first {
            return Ok((Token::Class { negated, ranges }, index + 1));
        }
        first = false;

        let (from, next) = read_class_char(chars, index)?;
        index = next;

        // A `-` that is not the last character starts a range.
        if chars.get(index) == Some(&'-') && chars.get(index + 1) != Some(&']') {
            let (to, next) = read_class_char(chars, index + 1)?;
            index = next;
            ranges.push((from, to));
        } else {
            ranges.push((from, from));
        }
    }
}

/// Reads one class member, which may be escaped.
fn read_class_char(chars: &[char], index: usize) -> Result<(char, usize), GlobError> {
    match chars.get(index) {
        None => Err(GlobError::UnclosedClass),
        Some('\\') => match chars.get(index + 1) {
            Some(&escaped) => Ok((escaped, index + 2)),
            None => Err(GlobError::TrailingEscape),
        },
        Some(&char) => Ok((char, index + 1)),
    }
}

fn is_separator(char: char) -> bool {
    char == '/'
}

fn match_from(tokens: &[Token], path: &[char], token_index: usize, path_index: usize) -> bool {
    let Some(token) = tokens.get(token_index) else {
        return path_index == path.len();
    };

    match token {
        Token::Literal(expected) => {
            path.get(path_index) == Some(expected) && match_from(tokens, path, token_index + 1, path_index + 1)
        }
        Token::AnyChar => {
            path.get(path_index).is_some_and(|&char| !is_separator(char))
                && match_from(tokens, path, token_index + 1, path_index + 1)
        }
        Token::AnySequence => {
            let mut end = path_index;
            loop {
                if match_from(tokens, path, token_index + 1, end) {
                    return true;
                }
                match path.get(end) {
                    Some(&char) if !is_separator(char) => end += 1,
                    _ => return false,
                }
            }
        }
        Token::AnySequenceDeep => {
            for end in path_index..=path.len() {
                if match_from(tokens, path, token_index + 1, end) {
                    return true;
                }
            }
            false
        }
        Token::Class { negated, ranges } => {
            let Some(&char) = path.get(path_index) else {
                return false;
            };
            if is_separator(char) {
                return false;
            }
            let contained = ranges
                .iter()
                .any(|&(from, to)| char >= from && char <= to);
            contained != *negated && match_from(tokens, path, token_index + 1, path_index + 1)
        }
    }
}
