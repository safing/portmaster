//! Protocol and port names, like Go `service/network/reference`.

const PROTOCOLS: &[(u8, &str)] = &[
    (1, "ICMP"),
    (2, "IGMP"),
    (6, "TCP"),
    (17, "UDP"),
    (27, "RDP"),
    (33, "DCCP"),
    (58, "ICMP6"),
    (136, "UDP-LITE"),
];

const PORT_NAMES: &[(u16, &str)] = &[
    (20, "FTP-DATA"),
    (21, "FTP"),
    (22, "SSH"),
    (23, "TELNET"),
    (25, "SMTP"),
    (43, "WHOIS"),
    (53, "DNS"),
    (67, "DHCP_SERVER"),
    (68, "DHCP_CLIENT"),
    (69, "TFTP"),
    (80, "HTTP"),
    (110, "POP3"),
    (123, "NTP"),
    (143, "IMAP"),
    (161, "SNMP"),
    (179, "BGP"),
    (194, "IRC"),
    (389, "LDAP"),
    (443, "HTTPS"),
    (445, "SMB"),
    (465, "SMTP_SSL"),
    (587, "SMTP_ALT"),
    (993, "IMAP_SSL"),
    (995, "POP3_SSL"),
];

// Go accepts both the dash and the underscore spelling for some names.
const PORT_ALIASES: &[(&str, u16)] = &[
    ("DHCP-SERVER", 67),
    ("DHCP-CLIENT", 68),
    ("SMTP-ALT", 587),
    ("SMTP-SSL", 465),
    ("IMAP-SSL", 993),
    ("POP3-SSL", 995),
];

/// Go `strings.ToUpper`: runes without a single-rune mapping stay unchanged.
fn go_upper(s: &str) -> String {
    s.chars()
        .map(|c| {
            let mut up = c.to_uppercase();
            match (up.next(), up.next()) {
                (Some(u), None) => u,
                _ => c,
            }
        })
        .collect()
}

/// Returns the name of a protocol number, or the number itself.
pub fn protocol_name(protocol: u8) -> String {
    PROTOCOLS
        .iter()
        .find(|(number, _)| *number == protocol)
        .map_or_else(|| protocol.to_string(), |(_, name)| (*name).to_string())
}

/// Returns the number of a protocol name (case-insensitive).
pub fn protocol_number(name: &str) -> Option<u8> {
    let name = go_upper(name);
    PROTOCOLS
        .iter()
        .find(|(_, n)| *n == name)
        .map(|(number, _)| *number)
}

/// Returns the name of a port number, or the number itself.
pub fn port_name(port: u16) -> String {
    PORT_NAMES
        .iter()
        .find(|(number, _)| *number == port)
        .map_or_else(|| port.to_string(), |(_, name)| (*name).to_string())
}

/// Returns the number of a port name (case-insensitive).
pub fn port_number(name: &str) -> Option<u16> {
    let name = go_upper(name);
    PORT_NAMES
        .iter()
        .map(|(number, n)| (*n, *number))
        .chain(PORT_ALIASES.iter().copied())
        .find(|(n, _)| *n == name)
        .map(|(_, number)| number)
}

/// Returns whether the protocol is packet based (has no streams).
pub fn is_packet_protocol(protocol: u8) -> bool {
    matches!(protocol, 1 | 17 | 27 | 58 | 33 | 136)
}

/// Returns whether the protocol is stream based.
pub fn is_stream_protocol(protocol: u8) -> bool {
    protocol == 6
}

/// Returns whether the protocol is ICMP or ICMPv6.
pub fn is_icmp(protocol: u8) -> bool {
    matches!(protocol, 1 | 58)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn names_and_numbers() {
        assert_eq!(protocol_name(6), "TCP");
        assert_eq!(protocol_name(127), "127");
        assert_eq!(protocol_number("udp-lite"), Some(136));
        assert_eq!(protocol_number("FOO"), None);
        assert_eq!(port_name(67), "DHCP_SERVER");
        assert_eq!(port_name(8080), "8080");
        assert_eq!(port_number("dhcp-server"), Some(67));
        assert_eq!(port_number("dhcp_server"), Some(67));
        assert_eq!(port_number("https"), Some(443));
        assert_eq!(port_number("NOPE"), None);
    }
}
