use core::ffi::c_void;

use crate::ffi::{    
    FwpsRedirectHandleCreate0, 
    FwpsRedirectHandleDestroy0, 
    FwpsQueryConnectionRedirectState0,
    FwpsAcquireClassifyHandle0,
    FwpsReleaseClassifyHandle0,
    FwpsPendClassify0,
    FWPS_CONNECT_REQUEST0,
    FWPS_CONNECTION_REDIRECT_STATE::* };

use super::{callout_data::CalloutData};

pub struct PendRedirectResult {
    pub classify_handle: u64,       // FwpsClassifyHandle from FwpsAcquireClassifyHandle0()
    pub filter_id: u64,             // Filter ID used for the pend operation
    pub classify_out: *mut c_void,  // ClassifyOut from FwpsPendClassify0()
}

// ============================================================================
// Redirect Handle Management
// ============================================================================

/// Wrapper for WFP redirect handle.
/// The redirect handle is required for local address modification in CONNECT_REDIRECT.
/// It must be created once at initialization and destroyed on cleanup.
pub struct RedirectController {
    redirect_handle: *mut c_void, // from FwpsRedirectHandleCreate0
}

impl RedirectController {
    /// Create a new redirect handle.
    /// The provider_guid should match your WFP provider GUID.
    pub fn new(provider_guid: u128) -> Result<Self, i32> {
        let mut handle: *mut c_void = core::ptr::null_mut();
        let status = unsafe {
            FwpsRedirectHandleCreate0(
                provider_guid as *const u128,
                0, // Reserved. Set to zero.
                &mut handle,
            )
        };
        if status != 0 {
            return Err(status);
        }
        Ok(Self { redirect_handle: handle })
    }

    /// Check if the connection has already been redirected.
    /// This helps prevent infinite redirect loops.
    ///     `redirect_records` is obtained from callout metadata.
    ///     `layer_data` is the pointer to the layer data (e.g. FwpsConnectRequest0).
    /// Returns true if the connection was already redirected by us or another local proxy.
    pub fn get_connection_is_redirected_state(&self, redirect_records: *const c_void, layer_data: *const c_void) -> bool {
        unsafe {
            let state = FwpsQueryConnectionRedirectState0(
                redirect_records,
                self.redirect_handle,
                core::ptr::null_mut(), // NULL, optional redirectContext
            );

            match state {
                FWPS_CONNECTION_REDIRECTED_BY_SELF | FWPS_CONNECTION_PREVIOUSLY_REDIRECTED_BY_SELF => {
                    // We already redirected this - do NOT redirect again (infinite loop!)                    
                    return true;
                },        
                FWPS_CONNECTION_REDIRECTED_BY_OTHER => {                    
                    // Another callout redirected this - check if it's a local proxy
                    let conn_req = layer_data as *const FWPS_CONNECT_REQUEST0;
                    if !(*conn_req).previous_version.is_null() &&
                    !(*(*conn_req).previous_version).local_redirect_handle.is_null() {
                        // Connection redirected by another callout to local proxy, permitting
                        return true;                   
                    }
                },
                FWPS_CONNECTION_NOT_REDIRECTED => {
                    // Not redirected yet - we can apply our redirect
                },
                FWPS_CONNECTION_REDIRECT_STATE_MAX => {
                    // Unknown state
                }
            }
        }
        false
    }

    pub fn pend(&self, data: &mut CalloutData) -> Result<PendRedirectResult, i32> {
        // Acquire classify handle
        let mut classify_handle: u64 = 0;
        let status = unsafe {
            FwpsAcquireClassifyHandle0(data.classify_out as *mut c_void, 0, &mut classify_handle)
        };
        if status != 0 {
            crate::err!("FwpsAcquireClassifyHandle0 failed: {:#x}", status);
            return Err(status);
        }
        
        // Pend classify operation
        let status = unsafe {
            FwpsPendClassify0(
                classify_handle,
                data.filter_id,
                0,
                data.classify_out)
        };
        if status != 0 {
            crate::err!("FwpsPendClassify0 failed: {:#x}", status);
            unsafe { FwpsReleaseClassifyHandle0(classify_handle); } // Release handle on failure!
            return Err(status);
        }

        return Ok(PendRedirectResult {
            classify_handle,
            filter_id: data.filter_id,
            classify_out: data.classify_out as *mut c_void,
        });
    }
}

impl Drop for RedirectController {
    fn drop(&mut self) {
        if !self.redirect_handle.is_null() {
            unsafe {
                FwpsRedirectHandleDestroy0(self.redirect_handle);
            }
            self.redirect_handle = core::ptr::null_mut();
        }
    }
}