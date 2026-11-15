use std::io;
use thiserror::Error;

/// Errors that can occur during resolver URL parsing.
#[derive(Debug, Error, PartialEq, Eq, Clone)]
pub enum ParseResolverError {
    #[error("DNS resolver scheme {0:?} invalid")]
    InvalidScheme(String),

    #[error("resolver IP {0:?} is invalid")]
    InvalidIp(String),

    #[error("invalid port {0:?}")]
    InvalidPort(String),

    #[error("cannot determine port for {0:?}")]
    CannotDeterminePort(String),

    #[error("unknown parameter \"\"{0}\"\"")]
    UnknownParameter(String),

    #[error("cannot set the domain name via both the hostname in the URL and the verify parameter")]
    DomainViaBothHostnameAndVerify,

    #[error("verify parameter must be set when using ip as domain")]
    VerifyMustBeSetWhenUsingIpAsDomain,

    #[error("cannot set the IP address via both the hostname in the URL and the ip parameter")]
    IpViaBothHostnameAndIpParam,

    #[error("domain verification is only supported by DoT and DoH servers")]
    DomainVerificationNotSupported,

    #[error("invalid value for upstream block detection (blockedif=)")]
    InvalidBlockDetection,

    #[error("search-only may only be used as an empty parameter")]
    SearchOnlyNotEmpty,

    #[error("cannot use search-only without search scopes")]
    SearchOnlyWithoutScopes,

    #[error("link-local may only be used as an empty parameter")]
    LinkLocalNotEmpty,

    #[error("failed to validate search domain #{0}: {1}")]
    SearchDomainValidation(usize, String),

    #[error("too many search domains, for security reasons only 100 search domains are supported")]
    TooManySearchDomains,

    #[error("{0}")]
    UrlParse(String),
}

/// Errors that can occur when resolving a DNS query over a transport.
#[derive(Debug, Error)]
pub enum ResolveError {
    #[error("timeout waiting for query")]
    Timeout,

    #[error("query was blocked upstream by {0}")]
    BlockedUpstream(String),

    #[error("I/O error: {0}")]
    Io(#[from] io::Error),

    #[error("protocol error: {0}")]
    Protocol(String),

    #[error("DNS decode error: {0}")]
    Decode(#[from] hickory_proto::serialize::binary::DecodeError),

    #[error("DNS protocol error: {0}")]
    Proto(#[from] hickory_proto::ProtoError),

    #[error("HTTP error: {0}")]
    Http(String),

    #[error("TLS error: {0}")]
    Tls(String),

    #[error("resolve failure: {0}")]
    Failure(String),

    #[error("record could not be found")]
    NotFound,

    #[error("resolver has no answer")]
    Continue,

    #[error("query was cancelled")]
    Cancelled,

    #[error("ID mismatch: expected {expected}, got {got}")]
    IdMismatch { expected: u16, got: u16 },
}
