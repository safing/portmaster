//! Port of Go `service/network/reference` and the domain checks of
//! `service/network/netutils`.

mod fqdn;
mod reference;

pub use fqdn::is_valid_fqdn;
pub use reference::{
    is_icmp, is_packet_protocol, is_stream_protocol, port_name, port_number, protocol_name,
    protocol_number,
};
