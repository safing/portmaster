//! Plaintext query parser, ported from `base/database/query/parser.go`.
//!
//! Special characters must be escaped with `\`: `(`, `)`, `"` and whitespace.

use crate::condition::{Condition, Value};
use crate::operators as op;
use crate::{Error, Query, Result};

/// A parsed token of a query, like Go's private `snippet`.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Snippet {
    /// Token text as bytes; the Go parser may cut off a trailing multi-byte
    /// character, which leaves invalid UTF-8 here, just like in Go.
    pub text: Vec<u8>,
    /// 1-based byte position of the token in the query text.
    pub position: usize,
}

impl Snippet {
    /// Returns the token text, replacing invalid UTF-8 like `encoding/json` does.
    pub fn text_lossy(&self) -> String {
        String::from_utf8_lossy(&self.text).into_owned()
    }
}

/// Parses a plaintext query into a checked [`Query`], like Go `ParseQuery`.
pub fn parse_query(query: &str) -> Result<Query> {
    let snippets = extract_snippets(query)?;
    let mut cursor = Cursor {
        all: &snippets,
        pos: 0,
        query_len: query.len(),
    };

    // Check for the query word.
    let query_word = cursor.next()?;
    if query_word.text != b"query" {
        return Err(Error::MissingQueryWord);
    }

    // Get the prefix.
    let prefix = cursor.next()?;
    let mut parsed = Query::new_raw(&prefix.text);

    while cursor.remaining() > 0 {
        let command = cursor.next()?;

        match command.text.as_slice() {
            b"where" => {
                if parsed.where_.is_some() {
                    return Err(Error::DuplicateClause {
                        clause: "where".to_string(),
                        position: command.position,
                    });
                }

                let condition = parse_and_or(&mut cursor, true)?;
                // Go steps back one snippet, as `parse_and_or` had to check
                // whether it is done.
                cursor.back();
                parsed.where_ = Some(condition);
            }
            b"orderby" => {
                if !parsed.order_by.is_empty() {
                    return Err(Error::DuplicateClause {
                        clause: "orderby".to_string(),
                        position: command.position,
                    });
                }

                let order_by = cursor.next()?;
                parsed.order_by = order_by.text.clone();
            }
            b"limit" => {
                if parsed.limit != 0 {
                    return Err(Error::DuplicateClause {
                        clause: "limit".to_string(),
                        position: command.position,
                    });
                }

                let snippet = cursor.next()?;
                let limit = parse_uint31(&snippet.text).ok_or_else(|| Error::Integer {
                    value: snippet.text_lossy(),
                    position: snippet.position,
                })?;
                parsed.limit = limit;
            }
            b"offset" => {
                if parsed.offset != 0 {
                    return Err(Error::DuplicateClause {
                        clause: "offset".to_string(),
                        position: command.position,
                    });
                }

                let snippet = cursor.next()?;
                let offset = parse_uint31(&snippet.text).ok_or_else(|| Error::Integer {
                    value: snippet.text_lossy(),
                    position: snippet.position,
                })?;
                parsed.offset = offset;
            }
            _ => {
                return Err(Error::UnknownClause {
                    clause: command.text_lossy(),
                    position: command.position,
                });
            }
        }
    }

    parsed.check()
}

struct Cursor<'a> {
    all: &'a [Snippet],
    pos: usize,
    query_len: usize,
}

impl<'a> Cursor<'a> {
    /// Like Go's `getSnippet`: advances first, then reports the end of input.
    fn next(&mut self) -> Result<&'a Snippet> {
        self.pos += 1;
        if self.pos > self.all.len() {
            return Err(Error::UnexpectedEnd(self.query_len));
        }
        Ok(&self.all[self.pos - 1])
    }

    fn back(&mut self) {
        self.pos -= 1;
    }

    fn remaining(&self) -> usize {
        self.all.len() - self.pos
    }
}

/// Splits a query into snippets, like Go `extractSnippets`.
pub fn extract_snippets(text: &str) -> Result<Vec<Snippet>> {
    let bytes = text.as_bytes();
    let mut snippets = Vec::new();
    let mut skip = false;
    let mut start: Option<usize> = None;
    let mut in_parenthesis = false;
    let mut pos = 0;

    for (index, ch) in text.char_indices() {
        pos = index;

        // Skip the character after a backslash.
        if skip {
            skip = false;
            continue;
        }
        if ch == '\\' {
            skip = true;
        }

        // Wait until the quoted token is closed.
        if in_parenthesis {
            if ch == '"' {
                if let Some(token_start) = start {
                    snippets.push(Snippet {
                        text: prep_token(&bytes[token_start + 1..index]),
                        position: token_start + 1,
                    });
                }
                start = None;
                in_parenthesis = false;
            }
            continue;
        }

        // Handle segments.
        match ch {
            '\t' | '\n' | '\r' | ' ' | '(' | ')' => {
                if let Some(token_start) = start {
                    snippets.push(Snippet {
                        text: prep_token(&bytes[token_start..index]),
                        position: token_start + 1,
                    });
                    start = None;
                }
            }
            _ => {
                if start.is_none() {
                    start = Some(index);
                }
            }
        }

        // Handle special segment characters.
        match ch {
            '(' | ')' => snippets.push(Snippet {
                text: ch.to_string().into_bytes(),
                position: index + 1,
            }),
            '"' => {
                if let Some(token_start) = start {
                    if token_start < index {
                        return Err(Error::QuoteInWord(index + 1));
                    }
                }
                in_parenthesis = true;
            }
            _ => {}
        }
    }

    // Add the last token. Go slices with `pos+1`, which cuts a trailing
    // multi-byte character in half; keep that behavior.
    if let Some(token_start) = start {
        let end = (pos + 1).min(bytes.len());
        snippets.push(Snippet {
            text: prep_token(&bytes[token_start..end]),
            position: token_start + 1,
        });
    }

    Ok(snippets)
}

/// Removes surrounding quotes and escape characters, like Go `prepToken`.
fn prep_token(raw: &[u8]) -> Vec<u8> {
    let trimmed = trim_quotes(raw);

    // Go uses the regexp `\\([^\\])` and replaces matches with the second byte.
    let mut out = Vec::with_capacity(trimmed.len());
    let mut index = 0;
    while index < trimmed.len() {
        if trimmed[index] == b'\\'
            && index + 1 < trimmed.len()
            && trimmed[index + 1] != b'\\'
        {
            out.push(trimmed[index + 1]);
            index += 2;
        } else {
            out.push(trimmed[index]);
            index += 1;
        }
    }
    out
}

fn trim_quotes(raw: &[u8]) -> &[u8] {
    let mut start = 0;
    let mut end = raw.len();
    while start < end && raw[start] == b'"' {
        start += 1;
    }
    while end > start && raw[end - 1] == b'"' {
        end -= 1;
    }
    &raw[start..end]
}

fn combine(mut conditions: Vec<Condition>, is_or: bool) -> Condition {
    if conditions.len() == 1 {
        return conditions.pop().expect("length was checked");
    }
    if is_or {
        Condition::or(conditions)
    } else {
        Condition::and(conditions)
    }
}

/// Parses a condition group, like Go `parseAndOr`.
fn parse_and_or(cursor: &mut Cursor<'_>, root_condition: bool) -> Result<Condition> {
    let mut is_or = false;
    let mut type_set = false;
    let mut wrap_in_not = false;
    let mut expecting_more = true;
    let mut conditions: Vec<Condition> = Vec::new();

    loop {
        if !expecting_more && root_condition && cursor.remaining() == 0 {
            // Go advances the cursor by one here, as the caller steps back.
            let _ = cursor.next();
            return Ok(combine(conditions, is_or));
        }

        let first = cursor.next()?;

        if !expecting_more && root_condition {
            match first.text.as_slice() {
                b"orderby" | b"limit" | b"offset" => return Ok(combine(conditions, is_or)),
                _ => {}
            }
        }

        match first.text.as_slice() {
            b"(" => {
                let condition = parse_and_or(cursor, false)?;
                if wrap_in_not {
                    conditions.push(Condition::not(condition));
                    wrap_in_not = false;
                } else {
                    conditions.push(condition);
                }
                expecting_more = true;
            }
            b")" => return Ok(combine(conditions, is_or)),
            b"and" => {
                if type_set && is_or {
                    return Err(Error::MixedAndOr(first.position));
                }
                is_or = false;
                type_set = true;
                expecting_more = true;
            }
            b"or" => {
                if type_set && !is_or {
                    return Err(Error::MixedAndOr(first.position));
                }
                is_or = true;
                type_set = true;
                expecting_more = true;
            }
            b"not" => {
                wrap_in_not = true;
                expecting_more = true;
            }
            _ => {
                let condition = parse_condition(first, cursor)?;
                if wrap_in_not {
                    conditions.push(Condition::not(condition));
                    wrap_in_not = false;
                } else {
                    conditions.push(condition);
                }
                expecting_more = false;
            }
        }
    }
}

fn parse_condition(first: &Snippet, cursor: &mut Cursor<'_>) -> Result<Condition> {
    let mut wrap_in_not = false;

    // Get the operator name.
    let mut op_name = cursor.next()?;
    if op_name.text == b"not" {
        wrap_in_not = true;
        op_name = cursor.next()?;
    }

    let Some(operator) = op::from_name(&op_name.text) else {
        return Err(Error::UnknownOperator(op_name.position));
    };

    // "exists" does not take a value.
    let condition = if operator == op::EXISTS {
        Condition::new_raw(&first.text, operator, Value::Null)
    } else {
        let value = cursor.next()?;
        Condition::new_raw(&first.text, operator, Value::Str(value.text.clone()))
    };

    Ok(if wrap_in_not {
        Condition::not(condition)
    } else {
        condition
    })
}

/// Parses an unsigned 31-bit integer, like Go `strconv.ParseUint(s, 10, 31)`.
fn parse_uint31(text: &[u8]) -> Option<usize> {
    if text.is_empty() || !text.iter().all(u8::is_ascii_digit) {
        return None;
    }
    let mut value: u64 = 0;
    for &digit in text {
        value = value.checked_mul(10)?.checked_add(u64::from(digit - b'0'))?;
        if value > i32::MAX as u64 {
            return None;
        }
    }
    Some(value as usize)
}
