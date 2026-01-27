use alloc::string::{String, ToString};
use protocol::info::Info;
use wdk::{
    filter_engine::{ 
        callout_data::CalloutData, 
        layer::{FieldsAleBindRedirectV4, FieldsAleBindRedirectV6},
        redirect::PendRedirectResult,
    },
};
use smoltcp::wire::{ IpAddress, IpProtocol, Ipv4Address, Ipv6Address };

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
    //  Todo: add more fields if needed...
}

// ============================================================================
// BIND_REDIRECT Layer Callouts
// ============================================================================

#[derive(Clone, Copy)]
struct BindRedirectInfo {    
    local_ip: IpAddress,
    local_port: u16,
    protocol: IpProtocol,
    process_id: u64,
}

/// Bind redirect data (only has local address info, no remote)
struct AleBindRedirectData {
    is_ipv6: bool,    
    info: BindRedirectInfo,
}

pub fn bind_redirect_v4(data: CalloutData) {
    type Fields = FieldsAleBindRedirectV4;

    let bind_data = AleBindRedirectData {
        is_ipv6: false,
        info: BindRedirectInfo {   
            process_id: data.get_process_id().unwrap_or(0),
            protocol: get_protocol(&data, Fields::IpProtocol as usize),
            local_ip: get_ipv4_address(&data, Fields::IpLocalAddress as usize),
            local_port: data.get_value_u16(Fields::IpLocalPort as usize),
        },        
    };

    ale_layer_bind_redirect(data, &bind_data);
}

pub fn bind_redirect_v6(data: CalloutData) {
    type Fields = FieldsAleBindRedirectV6;

    let bind_data = AleBindRedirectData {
        is_ipv6: true,
        info: BindRedirectInfo {
            process_id: data.get_process_id().unwrap_or(0),
            protocol: get_protocol(&data, Fields::IpProtocol as usize),
            local_ip: get_ipv6_address(&data, Fields::IpLocalAddress as usize),
            local_port: data.get_value_u16(Fields::IpLocalPort as usize),
        },        
    };

    ale_layer_bind_redirect(data, &bind_data);
}

/// Common ALE layer bind redirect handling
///
/// Using Bind or Connect Redirection:
/// https://learn.microsoft.com/en-us/windows-hardware/drivers/network/using-bind-or-connect-redirection
fn ale_layer_bind_redirect(mut data: CalloutData, bind_data: &AleBindRedirectData) {
    // Make the default path as block.
    data.action_block();

    let Some(device) = crate::entry::get_device() else {
        crate::dbg!("ERROR: ALE Bind Redirect: No device available.");
        return;
    };

    // Only handle TCP and UDP protocols
    if !matches!(bind_data.info.protocol, IpProtocol::Tcp | IpProtocol::Udp) {        
        data.action_permit();
        return;
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
    let pr_cache_id = device.redirect_cache.push(PendedRedirect { pend_redirect_result });

    crate::dbg!(
        "ALE Bind Redirect: PID={} {:?} {}:{} (id={})",
        bind_data.info.process_id,
        bind_data.info.protocol,
        bind_data.info.local_ip,
        bind_data.info.local_port,
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
    // Reuse the same redirection_request format, but with unspecified remote address
    let info = if bind_data.is_ipv6 {
        let local_ip = match bind_data.info.local_ip {
            IpAddress::Ipv6(ip) => ip.0,
            _ => return Err("Expected IPv6 address".to_string()),
        };
        Ok(protocol::info::redirection_request_v6(
            pr_cache_id,
            bind_data.info.process_id,
            u8::from(bind_data.info.protocol),
            local_ip,
            bind_data.info.local_port,
        ))
    } else {
        let local_ip = match bind_data.info.local_ip {
            IpAddress::Ipv4(ip) => ip.0,
            _ => return Err("Expected IPv4 address".to_string()),
        };
        Ok(protocol::info::redirection_request_v4(
            pr_cache_id,
            bind_data.info.process_id,
            u8::from(bind_data.info.protocol),
            local_ip,
            bind_data.info.local_port,
        ))
    };
    info
}