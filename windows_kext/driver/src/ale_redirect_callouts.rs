use protocol::info::Info;
use wdk::{
    filter_engine::{ 
        callout_data::CalloutData, 
        layer::{FieldsAleConnectRedirectV4, FieldsAleConnectRedirectV6},
        redirect::PendRedirectResult,
    },
};

use smoltcp::wire::{ IpAddress, IpProtocol, Ipv4Address, Ipv6Address };
use windows_sys::Win32::{ Foundation::HANDLE, };
use core::ffi::c_void;
use alloc::format;

use crate::connection::Direction;
#[derive(Clone, Copy, PartialEq, PartialOrd, Eq, Ord)]
pub struct RedirectInfo {    
    pub(crate)  local_ip: IpAddress,
    pub(crate)  local_port: u16,
    pub(crate)  remote_ip: IpAddress,
    pub(crate)  remote_port: u16,
    pub(crate)  protocol: IpProtocol,
    pub(crate)  process_id: u64,
}

struct AleRedirectData {
    is_ipv6: bool,    
    redirect_records: Option<HANDLE>,
    info: RedirectInfo,
}

fn get_protocol(data: &CalloutData, index: usize) -> IpProtocol {
    IpProtocol::from(data.get_value_u8(index))
}

fn get_ipv4_address(data: &CalloutData, index: usize) -> IpAddress {
    IpAddress::Ipv4(Ipv4Address::from_bytes(
        &data.get_value_u32(index).to_be_bytes(),
    ))
}

fn get_ipv6_address(data: &CalloutData, index: usize) -> IpAddress {
    IpAddress::Ipv6(Ipv6Address::from_bytes(data.get_value_byte_array16(index)))
}

/// Data stored for each pended redirect operation
pub struct PendedRedirect {
    pub pend_redirect_result: PendRedirectResult,
    // ... add more fields if needed
}

pub fn connect_redirect_v4(data: CalloutData) {
    type Fields = FieldsAleConnectRedirectV4;

    let ale_redirect_data = AleRedirectData {
        is_ipv6: false,
        redirect_records: data.get_redirect_records(),
        info: RedirectInfo {   
            process_id: data.get_process_id().unwrap_or(0),
            protocol: get_protocol(&data, Fields::IpProtocol as usize),
            local_ip: get_ipv4_address(&data, Fields::IpLocalAddress as usize),
            local_port: data.get_value_u16(Fields::IpLocalPort as usize),
            remote_ip: get_ipv4_address(&data, Fields::IpRemoteAddress as usize),
            remote_port: data.get_value_u16(Fields::IpRemotePort as usize),
        },        
    };

    ale_layer_connect_redirect(data, &ale_redirect_data);
}

pub fn connect_redirect_v6(data: CalloutData) {
    type Fields = FieldsAleConnectRedirectV6;

    let ale_redirect_data = AleRedirectData {
        is_ipv6: true,
        redirect_records: data.get_redirect_records(),
        info: RedirectInfo {
            process_id: data.get_process_id().unwrap_or(0),
            protocol: get_protocol(&data, Fields::IpProtocol as usize),
            local_ip: get_ipv6_address(&data, Fields::IpLocalAddress as usize),
            local_port: data.get_value_u16(Fields::IpLocalPort as usize),
            remote_ip: get_ipv6_address(&data, Fields::IpRemoteAddress as usize),
            remote_port: data.get_value_u16(Fields::IpRemotePort as usize),
        },        
    };

    ale_layer_connect_redirect(data, &ale_redirect_data);
}

/// Common ALE layer connect redirect handling
///
/// Using Bind or Connect Redirection:
/// https://learn.microsoft.com/en-us/windows-hardware/drivers/network/using-bind-or-connect-redirection
fn ale_layer_connect_redirect(mut data: CalloutData, ale_data: &AleRedirectData) {
    let Some(device) = crate::entry::get_device() else {
        crate::dbg!("ERROR: ALE Connect Redirect: No device available.");
        return;
    };

    if !matches!(ale_data.info.protocol, IpProtocol::Tcp | IpProtocol::Udp) {
        // Only TCP and UDP make sense to be supported in the ALE layer.
        // Everything else is not associated with a connection and will be handled in the packet layer.
        data.action_permit();
        return;
    }
    
    let is_loopback = match &ale_data.info.remote_ip {
        IpAddress::Ipv4(ip) => ip.is_loopback(),
        IpAddress::Ipv6(ip) => ip.is_loopback(),
    };    
    if is_loopback {
        data.action_permit();
        return;
    }

    crate::dbg!( "ALE Connect Redirect: PID={} Protocol={:?} IPv6={} Local={} Remote={} (fileter_id: {})",
            ale_data.info.process_id, ale_data.info.protocol, ale_data.is_ipv6,
            format!("{}:{}", ale_data.info.local_ip, ale_data.info.local_port),
            format!("{}:{}", ale_data.info.remote_ip, ale_data.info.remote_port),
            data.get_filter_id()
        );

    // Check if already redirected
    let is_redirected = device.redirector.get_connection_is_redirected_state(
        ale_data.redirect_records.unwrap_or(0 as HANDLE), 
        data.get_layer_data() as *const c_void);

    if is_redirected {
        crate::dbg!("ALE Connect Redirect: Connection already redirected by us or another local proxy, permitting.");
        data.action_permit();
        return;            
    }

    // Pend the redirect operation
    let pr_result = match device.redirector.pend(&mut data) {
        Ok(res) => res,
        Err(err_code) => {
            crate::err!("ALE Connect Redirect: pend failed: {:#x}", err_code);
            data.action_block();
            return;
        }
    };

    // Store the pended redirect info in the redirect cache
    let pr_cache_id = device.redirect_cache.push(
        PendedRedirect {
            pend_redirect_result: pr_result,
        });

    crate::dbg!("ALE Connect Redirect: Pended redirect stored in cache with ID {} (cache size: {})", pr_cache_id, device.redirect_cache.get_entries_count());

    let info = build_info(pr_cache_id, ale_data);
    if let Some(info) = info {
        if let Err(e) = device.event_queue.push(info) {
            // TODO: ... handle error?
            crate::err!("ALE Connect Redirect: Failed to push redirection request to event queue: {:?}", e);
        }
    } else {
        // TODO: ...invalid IP version combination?
        crate::err!("ALE Connect Redirect: Failed to build redirection request info: invalid IP version combination.");
    }

    // Block the operation until completed
    data.action_block();
    data.clear_write_flag();
}

/// Build redirection request info from ALE redirect data to be sent to user-mode.
fn build_info(pr_cache_id: u64, ale_data: &AleRedirectData) -> Option<Info> {
    let info = match (ale_data.info.local_ip, ale_data.info.remote_ip) {
        (IpAddress::Ipv6(local_ip), IpAddress::Ipv6(remote_ip)) if ale_data.is_ipv6 => {
            Some(protocol::info::redirection_request_v6(
                pr_cache_id,
                ale_data.info.process_id,
                Direction::Outbound as u8,
                u8::from(ale_data.info.protocol),
                local_ip.0,
                remote_ip.0,
                ale_data.info.local_port,            
                ale_data.info.remote_port,
            ))
        }
        (IpAddress::Ipv4(local_ip), IpAddress::Ipv4(remote_ip)) => {
            Some(protocol::info::redirection_request_v4(
                pr_cache_id,
                ale_data.info.process_id,
                Direction::Outbound as u8,
                u8::from(ale_data.info.protocol),
                local_ip.0,
                remote_ip.0,
                ale_data.info.local_port,            
                ale_data.info.remote_port,
            ))
        }
        _ => {
            None
        }
    };
    info
}