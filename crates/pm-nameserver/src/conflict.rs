//! Port of Go `conflict.go`: finding the software that holds the DNS port.

use std::net::{IpAddr, Ipv4Addr, Ipv6Addr};
use std::sync::Arc;

use pm_netstate::{IpProtocol, NetState, PacketInfo};
use pm_process::ProcReader;

/// The process that holds the listen address.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ConflictingProcess {
    pub name: String,
    pub exe: String,
}

impl ConflictingProcess {
    /// The description used in the notification.
    ///
    /// Like Go, the executable is only added to a non-empty name: without a
    /// name the description stays empty.
    pub fn description(&self) -> String {
        if self.name.is_empty() {
            String::new()
        } else if !self.exe.is_empty() {
            format!("{} ({})", self.name, self.exe)
        } else {
            self.name.clone()
        }
    }
}

/// Looks up the process listening on an address (Go `getListeningProcess`).
/// `Ok(None)` means nothing was found (or it is us).
pub trait ListeningProcessLookup: Send + Sync {
    fn listening_process(
        &self,
        ip: IpAddr,
        port: u16,
    ) -> Result<Option<ConflictingProcess>, String>;
}

/// The IPs that resolvers commonly listen on.
fn common_resolver_ips() -> [IpAddr; 5] {
    [
        IpAddr::V4(Ipv4Addr::UNSPECIFIED),
        IpAddr::V4(Ipv4Addr::LOCALHOST),
        IpAddr::V4(Ipv4Addr::new(127, 0, 0, 53)), // some resolvers on Linux
        IpAddr::V6(Ipv6Addr::UNSPECIFIED),
        IpAddr::V6(Ipv6Addr::LOCALHOST),
    ]
}

/// Finds the conflicting process, like Go `findConflictingProcess`.
pub fn find_conflicting_process(
    lookup: &dyn ListeningProcessLookup,
    ip: IpAddr,
    port: u16,
) -> Option<ConflictingProcess> {
    let ips: Vec<IpAddr> = if ip.is_unspecified() {
        common_resolver_ips().to_vec()
    } else {
        vec![ip]
    };

    for resolver_ip in ips {
        match lookup.listening_process(resolver_ip, port) {
            Err(err) => {
                // Log the error and try the next address.
                tracing::warn!("nameserver: failed to find conflicting service: {err}");
            }
            Ok(Some(process)) => return Some(process),
            Ok(None) => {}
        }
    }
    None
}

/// [`ListeningProcessLookup`] on the socket tables and the proc file system.
pub struct NetstateLookup {
    pub state: Arc<NetState>,
    pub procs: Arc<ProcReader>,
}

impl ListeningProcessLookup for NetstateLookup {
    fn listening_process(
        &self,
        ip: IpAddr,
        port: u16,
    ) -> Result<Option<ConflictingProcess>, String> {
        let unspecified = if ip.is_ipv4() {
            IpAddr::V4(Ipv4Addr::UNSPECIFIED)
        } else {
            IpAddr::V6(Ipv6Addr::UNSPECIFIED)
        };
        // Do not record the direction: the source is left empty.
        let mut packet = PacketInfo::new(unspecified, ip, IpProtocol::UDP, true);
        packet.dst_port = port;

        // There may be nothing listening on the port: treat errors as "not found".
        let Ok(found) = self.state.lookup(&packet, true) else {
            return Ok(None);
        };
        let pid = found.pid;
        if pid < 0 || u32::try_from(pid).ok() == Some(std::process::id()) {
            return Ok(None);
        }

        // The process may have disappeared already.
        let info = self.procs.read(pid).map_err(|e| e.to_string())?;
        Ok(Some(ConflictingProcess {
            name: info.name,
            exe: info.exe,
        }))
    }
}

#[cfg(test)]
mod tests {
    use std::sync::Mutex;

    use super::*;

    struct Recorder {
        asked: Mutex<Vec<IpAddr>>,
        answer_at: Option<IpAddr>,
        fail_first: bool,
    }

    impl ListeningProcessLookup for Recorder {
        fn listening_process(
            &self,
            ip: IpAddr,
            _port: u16,
        ) -> Result<Option<ConflictingProcess>, String> {
            let mut asked = self.asked.lock().unwrap();
            asked.push(ip);
            if self.fail_first && asked.len() == 1 {
                return Err("lookup failed".into());
            }
            Ok((Some(ip) == self.answer_at).then(|| ConflictingProcess {
                name: "resolved".into(),
                exe: "/usr/bin/resolved".into(),
            }))
        }
    }

    #[test]
    fn wildcard_checks_common_resolver_ips_in_order() {
        let lookup = Recorder {
            asked: Mutex::new(vec![]),
            answer_at: Some("127.0.0.53".parse().unwrap()),
            fail_first: true,
        };
        let found = find_conflicting_process(&lookup, "0.0.0.0".parse().unwrap(), 53).unwrap();
        assert_eq!(found.name, "resolved");
        let asked = lookup.asked.lock().unwrap();
        assert_eq!(
            *asked,
            vec![
                "0.0.0.0".parse::<IpAddr>().unwrap(),
                "127.0.0.1".parse().unwrap(),
                "127.0.0.53".parse().unwrap()
            ]
        );
    }

    #[test]
    fn specific_ip_checks_only_that_ip() {
        let lookup = Recorder {
            asked: Mutex::new(vec![]),
            answer_at: None,
            fail_first: false,
        };
        assert!(find_conflicting_process(&lookup, "127.0.0.17".parse().unwrap(), 53).is_none());
        assert_eq!(lookup.asked.lock().unwrap().len(), 1);
    }

    #[test]
    fn description_follows_go_quirk() {
        let p = ConflictingProcess {
            name: "a".into(),
            exe: "/x/a".into(),
        };
        assert_eq!(p.description(), "a (/x/a)");
        let p = ConflictingProcess {
            name: String::new(),
            exe: "/x/a".into(),
        };
        assert_eq!(p.description(), "");
    }
}
