//! Port of Go `nsutil` and the DNS-facing parts of `resolver/rrcache.go` and
//! `network/dns.go`: responders that craft replies, and providers of the
//! informational records in the extra section.

use std::str::FromStr;
use std::time::Duration;

use async_trait::async_trait;
use hickory_proto::op::{Message, OpCode, ResponseCode};
use hickory_proto::rr::rdata::{A, AAAA, TXT};
use hickory_proto::rr::{Name, RData, Record, RecordType};
use pm_network::Verdict;
use pm_resolver::rrcache::{now_unix, qtype_string};
use pm_resolver::RRCache;

/// The delay of NXDOMAIN replies, see RFC 4074 and Go `nsutil.NxDomain`.
pub const NXDOMAIN_DELAY: Duration = Duration::from_millis(500);

/// A responder crafts a DNS reply for a request that is not answered with
/// resolved data, like Go `nsutil.Responder`.
#[async_trait]
pub trait Responder: Send + Sync {
    /// Creates the reply. `None` means the request is dropped.
    async fn reply(&self, request: &Message) -> Option<Message>;
}

/// Adds records to the extra section of a reply, like Go `nsutil.RRProvider`.
pub trait RrProvider: Send + Sync {
    fn extra_rrs(&self, request: &Message) -> Vec<Record>;
}

/// The severity of an informational record.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Level {
    Info,
    Error,
}

impl Level {
    /// The lower case name Go prints for the level (`log.Severity.String`).
    pub fn name(self) -> &'static str {
        match self {
            Level::Info => "inf",
            Level::Error => "err",
        }
    }
}

/// The message cannot be made a TXT record.
#[derive(Debug, Clone, PartialEq, Eq, thiserror::Error)]
#[error("failed to parse record: {0}")]
pub struct MessageRecordError(String);

/// Decodes the message like the zone file parser Go builds the record with:
/// `\X` stands for `X` and `\DDD` for the byte with that decimal value. An
/// unescaped quote ends the string early, and a trailing backslash escapes
/// the closing quote; both are errors.
fn decode_zone_text(msg: &str) -> Result<Vec<u8>, MessageRecordError> {
    let bytes = msg.as_bytes();
    let mut out = Vec::with_capacity(bytes.len());
    let mut i = 0;
    while i < bytes.len() {
        match bytes[i] {
            b'\\' => {
                let Some(&next) = bytes.get(i + 1) else {
                    return Err(MessageRecordError("unterminated quoted string".to_string()));
                };
                if next.is_ascii_digit() {
                    let value = bytes
                        .get(i + 1..i + 4)
                        .filter(|d| d.iter().all(u8::is_ascii_digit))
                        .map(|d| d.iter().fold(0u32, |acc, c| acc * 10 + u32::from(c - b'0')))
                        .and_then(|v| u8::try_from(v).ok())
                        .ok_or_else(|| MessageRecordError("bad escape".to_string()))?;
                    out.push(value);
                    i += 4;
                } else {
                    out.push(next);
                    i += 2;
                }
            }
            b'"' => {
                return Err(MessageRecordError(
                    "unexpected data after quoted string".to_string(),
                ));
            }
            b => {
                out.push(b);
                i += 1;
            }
        }
    }
    Ok(out)
}

/// Creates an informational resource record for the extra section of a reply,
/// like Go `nsutil.MakeMessageRecord`. The record is a TXT record named
/// `<level>.portmaster.` with a TTL of 0.
pub fn make_message_record(level: Level, msg: &str) -> Result<Record, MessageRecordError> {
    let decoded = decode_zone_text(msg)?;
    let name = Name::from_str(&format!("{}.portmaster.", level.name()))
        .map_err(|e| MessageRecordError(e.to_string()))?;
    // Go splits long strings into chunks of 255 bytes.
    let chunks: Vec<&[u8]> = if decoded.is_empty() {
        vec![&[][..]]
    } else {
        decoded.chunks(255).collect()
    };
    Ok(Record::from_rdata(name, 0, RData::TXT(TXT::from_bytes(chunks))))
}

/// Adds informational records to the extra section of the reply, like Go
/// `nsutil.AddMessagesToReply`. Empty messages and messages that cannot be
/// made a record are skipped.
pub fn add_messages_to_reply<S: AsRef<str>>(reply: &mut Message, level: Level, msgs: &[S]) {
    for msg in msgs {
        let msg = msg.as_ref();
        if msg.is_empty() {
            continue;
        }
        match make_message_record(level, msg) {
            Ok(rr) => reply.additionals.push(rr),
            Err(err) => {
                tracing::trace!("nameserver: failed to add message to reply: {err}");
            }
        }
    }
}

/// Creates a reply with the given response code, like miekg `SetRcode`: the ID,
/// opcode, RD and CD bits and the first question are copied.
pub fn set_reply(request: &Message, rcode: ResponseCode) -> Message {
    let mut reply = Message::response(request.metadata.id, request.metadata.op_code);
    if request.metadata.op_code == OpCode::Query {
        reply.metadata.recursion_desired = request.metadata.recursion_desired;
        reply.metadata.checking_disabled = request.metadata.checking_disabled;
    }
    reply.metadata.response_code = rcode;
    if let Some(question) = request.queries.first() {
        reply.queries.push(question.clone());
    }
    reply
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum Kind {
    BlockIp,
    ZeroIp,
    Localhost,
    NxDomain,
    Refused,
    ServerFailure,
}

/// The responders of Go `nsutil`: [`StaticResponder::block_ip`] and friends.
#[derive(Debug, Clone)]
pub struct StaticResponder {
    kind: Kind,
    msgs: Vec<String>,
}

fn owned<S: AsRef<str>>(msgs: &[S]) -> Vec<String> {
    msgs.iter().map(|m| m.as_ref().to_string()).collect()
}

impl StaticResponder {
    fn new<S: AsRef<str>>(kind: Kind, msgs: &[S]) -> StaticResponder {
        StaticResponder {
            kind,
            msgs: owned(msgs),
        }
    }

    /// Replies with `0.0.0.17` or `::17` for each A or AAAA question. Without
    /// such a question the reply is NXDOMAIN.
    pub fn block_ip<S: AsRef<str>>(msgs: &[S]) -> StaticResponder {
        StaticResponder::new(Kind::BlockIp, msgs)
    }

    /// Replies with `0.0.0.0` or `::` for each A or AAAA question.
    pub fn zero_ip<S: AsRef<str>>(msgs: &[S]) -> StaticResponder {
        StaticResponder::new(Kind::ZeroIp, msgs)
    }

    /// Replies with the localhost addresses.
    pub fn localhost<S: AsRef<str>>(msgs: &[S]) -> StaticResponder {
        StaticResponder::new(Kind::Localhost, msgs)
    }

    /// Replies with NXDOMAIN, after a short delay.
    pub fn nx_domain<S: AsRef<str>>(msgs: &[S]) -> StaticResponder {
        StaticResponder::new(Kind::NxDomain, msgs)
    }

    /// Replies with REFUSED.
    pub fn refused<S: AsRef<str>>(msgs: &[S]) -> StaticResponder {
        StaticResponder::new(Kind::Refused, msgs)
    }

    /// Replies with SERVFAIL.
    pub fn server_failure<S: AsRef<str>>(msgs: &[S]) -> StaticResponder {
        StaticResponder::new(Kind::ServerFailure, msgs)
    }

    fn ip_reply(&self, request: &Message, a: A, aaaa: AAAA) -> Message {
        let mut answers = Vec::new();
        for question in &request.queries {
            match question.query_type() {
                RecordType::A => answers.push(Record::from_rdata(
                    question.name().clone(),
                    1,
                    RData::A(a),
                )),
                RecordType::AAAA => answers.push(Record::from_rdata(
                    question.name().clone(),
                    1,
                    RData::AAAA(aaaa),
                )),
                _ => {}
            }
        }

        // Without an A or AAAA question there is nothing to answer.
        let rcode = if answers.is_empty() {
            ResponseCode::NXDomain
        } else {
            ResponseCode::NoError
        };
        let mut reply = set_reply(request, rcode);
        reply.answers = answers;
        add_messages_to_reply(&mut reply, Level::Info, &self.msgs);
        reply
    }
}

#[async_trait]
impl Responder for StaticResponder {
    async fn reply(&self, request: &Message) -> Option<Message> {
        Some(match self.kind {
            Kind::BlockIp => self.ip_reply(
                request,
                A("0.0.0.17".parse().expect("valid IPv4")),
                AAAA("::17".parse().expect("valid IPv6")),
            ),
            Kind::ZeroIp => self.ip_reply(
                request,
                A("0.0.0.0".parse().expect("valid IPv4")),
                AAAA("::".parse().expect("valid IPv6")),
            ),
            Kind::Localhost => self.ip_reply(
                request,
                A("127.0.0.1".parse().expect("valid IPv4")),
                AAAA("::1".parse().expect("valid IPv6")),
            ),
            Kind::NxDomain => {
                let mut reply = set_reply(request, ResponseCode::NXDomain);
                add_messages_to_reply(&mut reply, Level::Info, &self.msgs);
                // Some nameservers wrongly answer NXDOMAIN instead of an empty
                // reply (RFC 4074); the delay mitigates the damage of that.
                tokio::time::sleep(NXDOMAIN_DELAY).await;
                reply
            }
            Kind::Refused => {
                let mut reply = set_reply(request, ResponseCode::Refused);
                add_messages_to_reply(&mut reply, Level::Info, &self.msgs);
                reply
            }
            Kind::ServerFailure => {
                let mut reply = set_reply(request, ResponseCode::ServFail);
                add_messages_to_reply(&mut reply, Level::Info, &self.msgs);
                reply
            }
        })
    }
}

/// Replies with the data of a resolved query, like Go `RRCache.ReplyWithDNS`.
pub struct RrCacheResponder<'a>(pub &'a RRCache);

#[async_trait]
impl Responder for RrCacheResponder<'_> {
    async fn reply(&self, request: &Message) -> Option<Message> {
        let mut reply = set_reply(request, self.0.rcode.into());
        reply.answers = self.0.answer.clone();
        reply.authorities = self.0.ns.clone();
        reply.additionals = self.0.extra.clone();
        Some(reply)
    }
}

/// Formats seconds like Go `time.Duration.String`, for whole seconds.
pub fn format_go_duration(total_secs: i64) -> String {
    if total_secs == 0 {
        return "0s".to_string();
    }
    let sign = if total_secs < 0 { "-" } else { "" };
    let secs = total_secs.unsigned_abs();
    let (h, m, s) = (secs / 3600, (secs % 3600) / 60, secs % 60);
    if h > 0 {
        format!("{sign}{h}h{m}m{s}s")
    } else if m > 0 {
        format!("{sign}{m}m{s}s")
    } else {
        format!("{sign}{s}s")
    }
}

/// Formats milliseconds like Go `time.Duration.String` of a duration rounded
/// to milliseconds (e.g. `1.4s`, `2m0.5s`, `250ms`).
pub fn format_go_duration_ms(total_ms: u64) -> String {
    if total_ms == 0 {
        return "0s".to_string();
    }
    if total_ms < 1000 {
        return format!("{total_ms}ms");
    }
    let (h, m) = (total_ms / 3_600_000, (total_ms % 3_600_000) / 60_000);
    let (s, ms) = ((total_ms % 60_000) / 1000, total_ms % 1000);
    let mut secs = s.to_string();
    if ms > 0 {
        secs.push('.');
        secs.push_str(format!("{ms:03}").trim_end_matches('0'));
    }
    if h > 0 {
        format!("{h}h{m}m{secs}s")
    } else if m > 0 {
        format!("{m}m{secs}s")
    } else {
        format!("{secs}s")
    }
}

/// Provides the informational records of a resolved query, like Go
/// `RRCache.GetExtraRRs`.
pub struct RrCacheExtras<'a>(pub &'a RRCache);

impl RrProvider for RrCacheExtras<'_> {
    fn extra_rrs(&self, _request: &Message) -> Vec<Record> {
        let cache = self.0;
        let mut extra = Vec::new();
        let mut add = |msg: &str| match make_message_record(Level::Info, msg) {
            Ok(rr) => extra.push(rr),
            Err(err) => {
                tracing::trace!("resolver: failed to add informational record to reply: {err}");
            }
        };

        let resolver_name = cache
            .resolver
            .as_ref()
            .map(|r| r.descriptive_name())
            .unwrap_or_default();
        if cache.served_from_cache {
            add(&format!("served from cache, resolved by {resolver_name}"));
        } else {
            add(&format!("freshly resolved by {resolver_name}"));
        }

        let now = now_unix();
        if cache.expires == 0 {
            add("record does not expire");
        } else if cache.expired() {
            add(&format!(
                "record expired since {}",
                format_go_duration(now - cache.expires)
            ));
        } else {
            add(&format!(
                "record valid for {}",
                format_go_duration(cache.expires - now)
            ));
        }
        if cache.requesting_new {
            add("async request to refresh the cache has been started");
        }
        if cache.is_backup {
            add("this record is served because a fresh request was unsuccessful");
        }

        if cache.filtered {
            if cache.filtered_entries.len() > 1 {
                add(&format!(
                    "{} RRs have been filtered:",
                    cache.filtered_entries.len()
                ));
            } else {
                add(&format!(
                    "{} RR has been filtered:",
                    cache.filtered_entries.len()
                ));
            }
            for entry in &cache.filtered_entries {
                add(entry);
            }
        }
        extra
    }
}

/// Replies to blocked, dropped or failed requests based on the verdict, like
/// Go `Connection.ReplyWithDNS`.
pub struct VerdictResponder {
    pub verdict: Verdict,
}

#[async_trait]
impl Responder for VerdictResponder {
    async fn reply(&self, request: &Message) -> Option<Message> {
        match self.verdict {
            Verdict::Block | Verdict::Failed => {
                StaticResponder::block_ip::<&str>(&[]).reply(request).await
            }
            // Do not respond to the request.
            Verdict::Drop => None,
            // Other verdicts are not expected when a request is denied.
            _ => {
                let mut reply = StaticResponder::server_failure::<&str>(&[])
                    .reply(request)
                    .await?;
                add_messages_to_reply(
                    &mut reply,
                    Level::Error,
                    &["INTERNAL ERROR: incorrect use of Connection DNS Responder"],
                );
                Some(reply)
            }
        }
    }
}

/// Provides the verdict record of a request, like Go
/// `Connection.GetExtraRRs`.
pub struct VerdictExtras {
    pub verdict: Verdict,
    pub reason: String,
    /// Records of the block reason (Go `Reason.Context` as `RRProvider`).
    pub reason_rrs: Vec<Record>,
}

impl RrProvider for VerdictExtras {
    fn extra_rrs(&self, _request: &Message) -> Vec<Record> {
        let level = if self.verdict == Verdict::Failed {
            Level::Error
        } else {
            Level::Info
        };
        let msg = format!("{}: {}", self.verdict.verb(), self.reason);
        match make_message_record(level, &msg) {
            Ok(rr) => {
                let mut extra = vec![rr];
                extra.extend(self.reason_rrs.iter().cloned());
                extra
            }
            Err(err) => {
                tracing::trace!("filter: failed to add informational record to reply: {err}");
                Vec::new()
            }
        }
    }
}

/// Returns the text of a response code, like miekg `RcodeToString`.
pub fn rcode_to_string(rcode: u16) -> &'static str {
    match rcode {
        0 => "NOERROR",
        1 => "FORMERR",
        2 => "SERVFAIL",
        3 => "NXDOMAIN",
        4 => "NOTIMP",
        5 => "REFUSED",
        6 => "YXDOMAIN",
        7 => "YXRRSET",
        8 => "NXRRSET",
        9 => "NOTAUTH",
        10 => "NOTZONE",
        11 => "DSOTYPENI",
        16 => "BADSIG",
        17 => "BADKEY",
        18 => "BADTIME",
        19 => "BADMODE",
        20 => "BADNAME",
        21 => "BADALG",
        22 => "BADTRUNC",
        23 => "BADCOOKIE",
        _ => "",
    }
}

/// The question type as text, like Go `dns.Type.String`.
pub fn question_text(qtype: RecordType) -> String {
    qtype_string(qtype)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn durations_format_like_go() {
        assert_eq!(format_go_duration_ms(0), "0s");
        assert_eq!(format_go_duration_ms(250), "250ms");
        assert_eq!(format_go_duration_ms(1400), "1.4s");
        assert_eq!(format_go_duration_ms(29_123), "29.123s");
        assert_eq!(format_go_duration_ms(30_000), "30s");
        assert_eq!(format_go_duration_ms(120_500), "2m0.5s");
        assert_eq!(format_go_duration_ms(3_600_000), "1h0m0s");
    }
}
