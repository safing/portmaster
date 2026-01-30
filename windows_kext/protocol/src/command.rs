// Commands from user space

use num_derive::FromPrimitive;
use num_traits::FromPrimitive;

#[repr(u8)]
#[derive(Clone, Copy, FromPrimitive)]
#[rustfmt::skip]
pub enum CommandType {
    Shutdown              = 0,
    Verdict               = 1,
    UpdateV4              = 2,
    UpdateV6              = 3,
    ClearCache            = 4,
    GetLogs               = 5,
    GetBandwidthStats     = 6,
    PrintMemoryStats      = 7,
    CleanEndedConnections = 8,

    /// Enables split tunneling functionality.
    /// 
    /// When enabled, the driver will:
    /// - Send BindRequest notifications for new connections
    /// - Allow SplitTunnel command to modify connection routing
    EnableSplitTunnel     = 9,
    /// Disables split tunneling functionality.
    /// 
    /// When disabled, the driver will:
    /// - Stop sending BindRequest notifications
    /// - SplitTunnel command will not have any effect.
    DisableSplitTunnel    = 10,
    /// Response to a bind redirect request (Split-Tunneling verdict).
    /// 
    /// This command is sent from user-space to the driver in response to
    /// BindRequest notifications. It tells the driver whether to:
    /// - Allow the original bind operation (no redirect)
    /// - Redirect the bind to a specific local IP address
    SplitTunnel           = 11,
}

#[repr(C, packed)]
pub struct Command {
    pub command_type: CommandType,
    value: [u8; 0],
}

#[repr(C, packed)]
#[derive(Debug, PartialEq, Eq)]
pub struct Verdict {
    pub id: u64,
    pub verdict: u8,
}

#[repr(C, packed)]
#[derive(Debug, PartialEq, Eq)]
pub struct UpdateV4 {
    pub protocol: u8,
    pub local_address: [u8; 4],
    pub local_port: u16,
    pub remote_address: [u8; 4],
    pub remote_port: u16,
    pub verdict: u8,
}

#[repr(C, packed)]
#[derive(Debug, PartialEq, Eq)]
pub struct UpdateV6 {
    pub protocol: u8,
    pub local_address: [u8; 16],
    pub local_port: u16,
    pub remote_address: [u8; 16],
    pub remote_port: u16,
    pub verdict: u8,
}

/// Response to a bind redirect request.
/// Contains the verdict on how to handle the bind operation.
#[repr(C, packed)]
#[derive(Debug, PartialEq, Eq)]
pub struct SplitTunnel {
    /// ID from the original InfoBindRequest notification
    pub id: u64,
    /// IPv4 local address to bind to.
    /// - Unspecified (0.0.0.0) - Allow original bind without redirect
    /// - Specific address - Redirect bind to this IPv4 address
    pub local_address_ipv4: [u8; 4],    
    /// IPv6 local address to bind to.
    /// - Unspecified (::) - Allow original bind without redirect
    /// - Specific address - Redirect bind to this IPv6 address
    pub local_address_ipv6: [u8; 16],    
}

pub fn parse_type(bytes: &[u8]) -> Option<CommandType> {
    FromPrimitive::from_u8(bytes[0])
}

pub fn parse_verdict(bytes: &[u8]) -> &Verdict {
    as_type(bytes)
}

pub fn parse_update_v4(bytes: &[u8]) -> &UpdateV4 {
    as_type(bytes)
}

pub fn parse_update_v6(bytes: &[u8]) -> &UpdateV6 {
    as_type(bytes)
}

pub fn parse_split_tunnel(bytes: &[u8]) -> &SplitTunnel {
    as_type(bytes)
}

fn as_type<T>(bytes: &[u8]) -> &T {
    let ptr: *const u8 = &bytes[0];
    let t_ptr: *const T = ptr as _;
    unsafe { t_ptr.as_ref().unwrap() }
}

#[cfg(test)]
use std::fs::File;
#[cfg(test)]
use std::io::Read;
#[cfg(test)]
use std::mem::size_of;
#[cfg(test)]
use std::panic;

#[test]
fn test_go_command_file() {
    let mut file = File::open("testdata/go_command_test.bin").unwrap();
    loop {
        let mut command: [u8; 1] = [0];
        let bytes_count = file.read(&mut command).unwrap();
        if bytes_count == 0 {
            return;
        }
        if let Some(command) = parse_type(&command) {
            match command {
                CommandType::Shutdown => {}
                CommandType::Verdict => {
                    let mut buf = [0; size_of::<Verdict>()];
                    let bytes_count = file.read(&mut buf).unwrap();
                    if bytes_count != size_of::<Verdict>() {
                        panic!("unexpected bytes count")
                    }

                    assert_eq!(parse_verdict(&buf), &Verdict { id: 1, verdict: 2 })
                }
                CommandType::UpdateV4 => {
                    let mut buf = [0; size_of::<UpdateV4>()];
                    let bytes_count = file.read(&mut buf).unwrap();
                    if bytes_count != size_of::<UpdateV4>() {
                        panic!("unexpected bytes count")
                    }

                    assert_eq!(
                        parse_update_v4(&buf),
                        &UpdateV4 {
                            protocol: 1,
                            local_address: [1, 2, 3, 4],
                            local_port: 2,
                            remote_address: [2, 3, 4, 5],
                            remote_port: 3,
                            verdict: 4
                        }
                    )
                }
                CommandType::UpdateV6 => {
                    let mut buf = [0; size_of::<UpdateV6>()];
                    let bytes_count = file.read(&mut buf).unwrap();
                    if bytes_count != size_of::<UpdateV6>() {
                        panic!("unexpected bytes count")
                    }

                    assert_eq!(
                        parse_update_v6(&buf),
                        &UpdateV6 {
                            protocol: 1,
                            local_address: [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16],
                            local_port: 2,
                            remote_address: [
                                2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17
                            ],
                            remote_port: 3,
                            verdict: 4
                        }
                    )
                }
                CommandType::ClearCache => {}
                CommandType::GetLogs => {}
                CommandType::GetBandwidthStats => {}
                CommandType::PrintMemoryStats => {}
                CommandType::CleanEndedConnections => {}
                CommandType::SplitTunnel => {}
                CommandType::EnableSplitTunnel => {}
                CommandType::DisableSplitTunnel => {}
            }
        } else {
            panic!("Unknown command: {}", command[0]);
        }
    }
}
