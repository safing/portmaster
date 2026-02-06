use alloc::string::{String};
use protocol::info::Info;
use wdk::{
    filter_engine::{ 
        callout_data::CalloutData, 
        layer::{FieldsAleBindRedirectV4, FieldsAleBindRedirectV6, FieldsAleConnectRedirectV4, FieldsAleConnectRedirectV6},
        redirect::PendRedirectResult,
        redirect::RedirectLayer,
    },
};
use smoltcp::wire::{ IpAddress, IpProtocol, Ipv4Address, Ipv6Address };

use crate::ale_redirects_cache::BindRedirectKey;

const IPV4_LOOPBACK: IpAddress = IpAddress::Ipv4(Ipv4Address([127, 0, 0, 1]));
const IPV6_LOOPBACK: IpAddress = IpAddress::Ipv6(Ipv6Address::LOOPBACK);

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
    pub key: BindRedirectKey,
    pub ipv6: bool
}

// ============================================================================
// BIND_REDIRECT Layer Callouts
// ============================================================================
/// Bind redirect data (only has local address info, no remote)
struct AleBindRedirectData {
    is_ipv6: bool,
    pub process_id: u64,
    pub protocol: IpProtocol,
    pub local_address: IpAddress,   // usually zeroed at ALE_BIND_REDIRECT stage
    pub local_port: u16,            // usually zeroed at ALE_BIND_REDIRECT stage
}

pub fn bind_redirect_v4(data: CalloutData) {
    type Fields = FieldsAleBindRedirectV4;

    let bind_data = AleBindRedirectData {
        is_ipv6: false,
        process_id: data.get_process_id().unwrap_or(0),
        protocol: get_protocol(&data, Fields::IpProtocol as usize),
        local_address: get_ipv4_address(&data, Fields::IpLocalAddress as usize),
        local_port: data.get_value_u16(Fields::IpLocalPort as usize),
    };

    ale_layer_bind_redirect(data, &bind_data);
}

pub fn bind_redirect_v6(data: CalloutData) {
    type Fields = FieldsAleBindRedirectV6;

    let bind_data = AleBindRedirectData {
        is_ipv6: true,
        process_id: data.get_process_id().unwrap_or(0),
        protocol: get_protocol(&data, Fields::IpProtocol as usize),
        local_address: get_ipv6_address(&data, Fields::IpLocalAddress as usize),
        local_port: data.get_value_u16(Fields::IpLocalPort as usize),
    };

    ale_layer_bind_redirect(data, &bind_data);
}

/// Common ALE layer bind redirect handling
/// https://learn.microsoft.com/en-us/windows-hardware/drivers/network/using-bind-or-connect-redirection
fn ale_layer_bind_redirect(mut data: CalloutData, bind_data: &AleBindRedirectData) {
    // Make the default path as block.
    data.action_block();

    let Some(device) = crate::entry::get_device() else {
        crate::err!("ERROR: ALE Bind Redirect: No device available.");
        return;
    };

    // Only handle TCP and UDP protocols
    if !matches!(bind_data.protocol, IpProtocol::Tcp | IpProtocol::Udp) {        
        data.action_permit();
        return;
    }

    // Skip localhost/loopback addresses - no need to redirect
    let is_loopback = match bind_data.local_address {
            IpAddress::Ipv4(ip) => ip.is_loopback(),
            IpAddress::Ipv6(ip) => ip.is_loopback(),
        };
    if is_loopback {
        data.action_permit();
        return;
    }
    
    // Check if we already have bind verdict for this PID
    let bind_key = BindRedirectKey::new(bind_data.process_id);
    if let Some(bind_verdict) = device.bind_redirect_cache.get(bind_key) {
        match bind_verdict.get_address(bind_data.is_ipv6) {
            None => {
                // No bind redirect. Allow original bind
                data.action_permit();
                return;
            }
            Some(addr) => {
                if addr.eq(&bind_data.local_address) {
                    // cached verdict matches requested local address - permit, no redirection needed    
                    data.action_permit();
                    return;
                }
                // We have already valid redirection verdict for this bind.
                // Do redirection and do not notify user-mode again.
                match device.redirector.redirect(&mut data, addr, RedirectLayer::BindRedirect)
                {
                    Ok(()) => {
                        crate::dbg!("ALE Bind Redirect: pid={} {} => {} (apply cached redirection)",
                            bind_data.process_id, bind_data.protocol, addr );
                        return
                    },
                    Err(err_code) => {
                        crate::err!("ALE Bind Redirect: redirect failed (cached verdict): {:#x}", err_code);
                        // Fall through to pending redirect so user-mode can decide again.
                    }
                } 
            }
        }
    }

    // Pend the bind redirect operation
    let pend_redirect_result = match device.redirector.pend(&mut data) {
        Ok(res) => res,
        Err(err_code) => {
            crate::err!("ALE Bind Redirect: pend failed: {:#x}", err_code);
            return;
        }
    };

    // Store the pended redirect info in the redirect cache
    let pr_cache_id = device.redirect_cache.push(PendedRedirect {
            pend_redirect_result,
            key: bind_key,
            ipv6: bind_data.is_ipv6,
        });

    crate::dbg!("ALE Bind Redirect: PID={} {:?} {}:{} (id={})",
        bind_data.process_id,
        bind_data.protocol,
        bind_data.local_address,
        bind_data.local_port,
        pr_cache_id
    );

    if device.redirect_cache.get_entries_count() >= 1000 {
        crate::warn!("ALE Bind Redirect: WARNING - redirect cache size is large: {}", device.redirect_cache.get_entries_count());
    }
        
    // Build redirection request info to be sent to user-mode
    let result = match build_bind_info(pr_cache_id, bind_data) {
        Ok(info) => {
            device.event_queue.push(info)
                .map_err(|e| { crate::err!("ALE Bind Redirect: Failed to push request to event queue: {:?}", e); })
        }
        Err(err) => {
            crate::err!("ALE Bind Redirect: Failed to build request info: {}", err);
            Err(())
        }    
    };

    if result.is_err() {
        // An error occurred, cancel the pended bind redirect operation
        let pr = device.redirect_cache.pop_id(pr_cache_id); 
        if let Some(pr) = pr { 
            device.redirector.cancel_pend(pr.pend_redirect_result);
        } else {
            crate::err!("ALE Bind Redirect (INTERNAL ERROR): Failed to pop redirect cache entry for id {}", pr_cache_id);
        }
        return;
    }

    // Block the operation until completed
    data.action_block();
    data.clear_write_flag();
}

/// Build bind redirection request info to be sent to user-mode.
fn build_bind_info(pr_cache_id: u64, bind_data: &AleBindRedirectData) -> Result<Info, String> {
    Ok(protocol::info::bind_request(pr_cache_id, bind_data.process_id))
}

// ============================================================================
// CONNECT_REDIRECT Layer Callouts
// ============================================================================
/// Connect redirect data (has local and remote address info)
#[derive(Clone, Copy, PartialEq, PartialOrd, Eq, Ord)]
pub struct AleConnectRedirectData {
    is_ipv6: bool,
    pub(crate)  local_ip: IpAddress,
    pub(crate)  local_port: u16,
    pub(crate)  remote_ip: IpAddress,
    pub(crate)  remote_port: u16,
    pub(crate)  protocol: IpProtocol,
    pub(crate)  process_id: u64,
}

pub fn connect_redirect_v4(data: CalloutData) {
    type Fields = FieldsAleConnectRedirectV4;

    let ale_redirect_data = AleConnectRedirectData {
        is_ipv6: false,
        //redirect_records: data.get_redirect_records(),
        process_id: data.get_process_id().unwrap_or(0),
        protocol: get_protocol(&data, Fields::IpProtocol as usize),
        local_ip: get_ipv4_address(&data, Fields::IpLocalAddress as usize),
        local_port: data.get_value_u16(Fields::IpLocalPort as usize),
        remote_ip: get_ipv4_address(&data, Fields::IpRemoteAddress as usize),
        remote_port: data.get_value_u16(Fields::IpRemotePort as usize),
    };

    ale_layer_connect_redirect(data, &ale_redirect_data);
}

pub fn connect_redirect_v6(data: CalloutData) {
    type Fields = FieldsAleConnectRedirectV6;

    let ale_redirect_data = AleConnectRedirectData {
        is_ipv6: true,
        //redirect_records: data.get_redirect_records(),
        process_id: data.get_process_id().unwrap_or(0),
        protocol: get_protocol(&data, Fields::IpProtocol as usize),
        local_ip: get_ipv6_address(&data, Fields::IpLocalAddress as usize),
        local_port: data.get_value_u16(Fields::IpLocalPort as usize),
        remote_ip: get_ipv6_address(&data, Fields::IpRemoteAddress as usize),
        remote_port: data.get_value_u16(Fields::IpRemotePort as usize),
    };

    ale_layer_connect_redirect(data, &ale_redirect_data);
}

fn ale_layer_connect_redirect(mut data: CalloutData, ale_data: &AleConnectRedirectData) {
    // Make the default path as permit.
    data.action_permit();

    let Some(device) = crate::entry::get_device() else {
        return;
    };

    if !matches!(ale_data.protocol, IpProtocol::Tcp | IpProtocol::Udp) {
        return;
    }

    let is_remote_loopback = match &ale_data.remote_ip {
        IpAddress::Ipv4(ip) => ip.is_loopback(),
        IpAddress::Ipv6(ip) => ip.is_loopback(),
    };

    // Check only one specific case:
    // - Remote is loopback but local is not loopback.
    // Other cases are ignored.

    if !is_remote_loopback {
        // Remote is not loopback, no special handling needed.
        return;
    }
    
    let is_local_loopback = match &ale_data.local_ip {
        IpAddress::Ipv4(ip) => ip.is_loopback(),
        IpAddress::Ipv6(ip) => ip.is_loopback(),
    };

    if is_local_loopback {
        // Local is loopback, remote is loopback - looks like normal local connection, nothing to do.
        return;
    }

    // Looks like collision with loopback address:
    // destination loopback address should not be used in with non-loopback local address.
    // It seems we changed local address in ALE_BIND_REDIRECT layer.
    // If so - revert source address to loopback.
    
    // Check if we already have bind verdict for this PID
    let bind_key = BindRedirectKey::new(ale_data.process_id);
    if let Some(bind_verdict) = device.bind_redirect_cache.get(bind_key) {
        match bind_verdict.get_address(ale_data.is_ipv6) {
            None => {
                // We did not touch local address in bind redirect.
                // Nothing to revert.
            }
            Some(_) => {
                // We have already valid redirection verdict for this bind.
                // So the local address was changed by us in bind redirect. 
                // We should revert it back to loopback.
                let loopback_addr = if ale_data.is_ipv6 { IPV6_LOOPBACK } else { IPV4_LOOPBACK };

                match device.redirector.redirect(&mut data, loopback_addr, RedirectLayer::ConnectRedirect)
                {
                    Ok(()) => {
                        crate::dbg!("ALE Connect Redirect: restore source to loopback. pid={} {:?} {}:{} => {}:{}",
                            ale_data.process_id, ale_data.protocol, loopback_addr, ale_data.local_port, ale_data.remote_ip, ale_data.remote_port );
                    },
                    Err(err_code) => {
                        crate::err!("ALE Connect Redirect: restore source to loopback failed (pid={} {:?} {}:{} => {}:{}): {:#x}",
                            ale_data.process_id, ale_data.protocol, loopback_addr, ale_data.local_port, ale_data.remote_ip, ale_data.remote_port, err_code );
                    }
                } 
            }
        }
    }
}