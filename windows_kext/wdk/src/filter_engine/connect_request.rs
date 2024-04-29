use core::ffi::c_void;

use windows_sys::Win32::{
    Foundation::HANDLE,
    Networking::WinSock::{AF_INET, AF_INET6},
};

use crate::info;

#[repr(C)]
pub(crate) struct FwpsConnectRequest0 {
    pub(crate) local_address_and_port: [u8; 128],
    pub(crate) remote_address_and_port: [u8; 128],
    pub(crate) port_reservation_token: u64,
    pub(crate) local_redirect_target_pid: u32,
    pub(crate) previous_version: *const FwpsConnectRequest0,
    pub(crate) modifier_filter_id: u64,
    pub(crate) local_redirect_handle: HANDLE,
    pub(crate) local_redirect_context: *mut c_void,
    pub(crate) local_redirect_context_size: usize,
}

#[repr(C)]
struct SocketAddressGeneric {
    family: u16,
    padding: [u8; 128 - 2],
}

#[repr(C)]
struct SocketAddressIPv4 {
    family: u16,
    port: u16,
    addr: [u8; 4],
    zero: [u8; 8],
    padding: [u8; 128 - 2 - 2 - 4 - 8],
}

#[repr(C)]
struct SocketAddressIPv6 {
    family: u16,
    port: u16,
    flowinfo: u16,
    addr: [u8; 16],
    scope_id: u32,
    padding: [u8; 128 - 2 - 2 - 2 - 16 - 4],
}

impl FwpsConnectRequest0 {
    pub(crate) fn set_remote(&mut self, ip: &[u8], port: u16) {
        info!("local: {:?}", self.local_address_and_port);
        info!("remote: {:?}", self.remote_address_and_port);
        unsafe {
            let generic_socket: &mut SocketAddressGeneric =
                core::mem::transmute(&mut self.remote_address_and_port);
            match generic_socket.family {
                AF_INET => {
                    info!("Socket type AF_INET");
                    let socket_ipv4: &mut SocketAddressIPv4 = core::mem::transmute(generic_socket);
                    for i in 0..4 {
                        socket_ipv4.addr[i] = ip[i];
                    }
                    socket_ipv4.port = u16::to_be(port);
                }
                AF_INET6 => {
                    info!("Socket type AF_INET6");
                    let socket_ipv6: &mut SocketAddressIPv6 = core::mem::transmute(generic_socket);
                    for i in 0..16 {
                        socket_ipv6.addr[i] = ip[i];
                    }
                    socket_ipv6.port = u16::to_be(port);
                }
                _ => {
                    info!("Unsupported socket type: {}", generic_socket.family);
                }
            }
        }
        info!("after: {:?}", self.remote_address_and_port);
    }
}
