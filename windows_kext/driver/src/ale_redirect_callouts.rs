use alloc::string::{String};
use protocol::info::Info;
use wdk::{
    filter_engine::{ 
        callout_data::CalloutData, 
        layer::{FieldsAleBindRedirectV4, FieldsAleBindRedirectV6},
        redirect::PendRedirectResult,
    },
};
use smoltcp::wire::{ IpAddress, IpProtocol, Ipv4Address, Ipv6Address };

use crate::ale_redirects_cache::BindRedirectKey;

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
                match device.redirector.redirect(&mut data, addr)
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