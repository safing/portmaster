//! Interoperability with third-party VPN clients for the Portmaster core.
//!
//! This crate is the Rust port of Go `service/interop` and
//! `service/interop/ivpn`: the `interop/ping` API, IVPN daemon compatibility
//! (connection lifecycle, event handlers, DNS coordination, verdicts) and the
//! IVPN notifications. All identifiers, messages and database keys are kept
//! byte-compatible with the Go implementation.
//!
//! Network and filesystem access is hidden behind the [`IvpnClient`] and
//! [`IvpnClientFactory`] traits so the logic can be tested with mocks. The real
//! implementation speaks the daemon's newline-delimited JSON protocol on
//! `127.0.0.1`.
//!
//! The Linux nftables/iptables WireGuard kill-switch compatibility hook from Go
//! `hook_linux.go` is ported in [`hook`] as a gracefully-degradable module: an
//! injectable [`hook::CommandRunner`] runs `nft`/`iptables`, and a missing tool,
//! missing table or permission error is only logged as a warning. Windows and
//! eBPF-specific hooks remain out of scope.

mod client;
mod engine;
mod env;
pub mod hook;
mod module;
mod notifications;
mod ping;
mod protocol;
mod status;

pub use client::{
    read_port_info, ClientEvent, IvpnClient, IvpnClientFactory, TcpIvpnClient,
    TcpIvpnClientFactory,
};
pub use engine::{InteropIvpn, DNS_LOCKED_DESCRIPTION};
pub use env::{InteropEnv, TestEnv};
pub use hook::{
    parse_nft_insert_handle, CommandOutput, CommandRunner, HookConfig, NftHook,
    SystemCommandRunner, BYPASS_COMMENT, NFT_TABLE,
};
pub use module::Interop;
pub use notifications::{
    build_ivpn_notification, build_old_version_notification, ACTION_ID_ACK, ACTION_ID_SUPPRESS,
    ACTION_TEXT_ACK, ACTION_TEXT_SUPPRESS, EVENT_ID_IVPN, EVENT_ID_IVPN_OLD_VERSION,
    MESSAGE_IVPN, MESSAGE_OLD_VERSION, NOTIFICATION_DB_ID_IVPN_DETECT_SUPPRESSED,
    OLD_VERSION_EXPIRY_SECS, TITLE_IVPN, TITLE_OLD_VERSION,
};
pub use ping::{register_ping_api, API_DESCRIPTION_PING, API_ENDPOINT_PING, API_NAME_PING};
pub use protocol::{
    ip_protocol_name, parse_command_base, parse_connected_resp, parse_connection_starting,
    parse_hello_resp, CommandBase, ConnectedResp, ConnectionStarting, DnsServerConfig, DnsSettings,
    HelloResp, CLIENT_TYPE_PORTMASTER, CMD_HELLO, CMD_SET_TEMP_PRIORITIZED_DNS,
    MSG_CONNECTION_STARTING, MSG_CONNECTION_STOPPED, MSG_CONNECTED_RESP, PROTOCOL_TCP,
    PROTOCOL_UDP, VPN_TYPE_OPENVPN, VPN_TYPE_WIREGUARD,
};
pub use status::{
    decide, ClientStatus, ConnView, EntityView, VerdictDecision, VpnConnectionInfo,
};
