use alloc::string::{String, ToString};
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

use crate::connection::Direction;
use crate::connection_map::Key;

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
    pub key: Key,
    pub process_id: u64,
    pub direction: Direction,
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
    // Make the default path as block.
    data.action_block();

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

    // Check if already redirected
    let is_redirected = device.redirector.get_connection_is_redirected_state(
        ale_data.redirect_records.unwrap_or(0 as HANDLE), 
        data.get_layer_data() as *const c_void);

    if is_redirected {
        // ALE Connect Redirect: Connection already redirected by us or another local proxy, permitting.
        data.action_permit();
        return;            
    }

    // Pend the redirect operation
    let pend_redirect_result = match device.redirector.pend(&mut data) {
        Ok(res) => res,
        Err(err_code) => {
            crate::err!("ALE Connect Redirect: pend failed: {:#x}", err_code);
            return;
        }
    }; 

    // Build connection key for later use 
    let key = Key {
        protocol: ale_data.info.protocol,
        local_address: ale_data.info.local_ip,
        local_port: ale_data.info.local_port,
        remote_address: ale_data.info.remote_ip,
        remote_port: ale_data.info.remote_port,
    };

    // Store the pended redirect info in the redirect cache
    let pr_cache_id = device.redirect_cache.push(PendedRedirect {
        pend_redirect_result,
        key,
        process_id: ale_data.info.process_id,
        direction: Direction::Outbound,
    });

    crate::dbg!(
        "ALE Connect Redirect: PID={} {:?} {}:{} -> {}:{} (id={})",
        ale_data.info.process_id,
        ale_data.info.protocol,
        ale_data.info.local_ip,
        ale_data.info.local_port,
        ale_data.info.remote_ip,
        ale_data.info.remote_port,
        pr_cache_id
    );

    if device.redirect_cache.get_entries_count() >= 1000 {
        crate::warn!("ALE Connect Redirect: WARNING - redirect cache size is large: {}", device.redirect_cache.get_entries_count());
    }
        
    // Build redirection request info to be sent to user-mode
    let result = match build_info(pr_cache_id, ale_data) {
        Ok(info) => {
             // Push the redirection request info to the event queue to be sent to user-mode
            device.event_queue.push(info)
                .map_err(|e| { crate::err!("ALE Connect Redirect: Failed to push redirection request to event queue: {:?}", e); })
        }
        Err(err) => {
            crate::err!("ALE Connect Redirect: Failed to build redirection request info: {}", err);
            Err(())
        }    
    };

    if result.is_err() {
        // An error occurred, cancel the pended redirect operation
        // Pop the redirect cache entry
        let pr = device.redirect_cache.pop_id(pr_cache_id); 
        if let Some(pr) = pr { 
            // Cancel the pended redirect operation and release resources
            device.redirector.cancel_pend(pr.pend_redirect_result);
        } else {
            // This should never happen
            crate::err!("ALE Connect Redirect (INTERNAL ERROR): Failed to pop redirect cache entry for id {}", pr_cache_id);
        }
        return;
    }

    // Block the operation until completed
    data.action_block();
    data.clear_write_flag();
}

/// Build redirection request info from ALE redirect data to be sent to user-mode.
fn build_info(pr_cache_id: u64, ale_data: &AleRedirectData) -> Result<Info, String> {
    let info = match (ale_data.info.local_ip, ale_data.info.remote_ip) {
        (IpAddress::Ipv6(local_ip), IpAddress::Ipv6(remote_ip)) if ale_data.is_ipv6 => {
            Ok(protocol::info::redirection_request_v6(
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
            Ok(protocol::info::redirection_request_v4(
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
            Err("Invalid IP version combination".to_string())                
        }
    };
    info
}