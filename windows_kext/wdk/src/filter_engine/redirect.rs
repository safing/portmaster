use core::ffi::c_void;
use smoltcp::wire::IpAddress;

use crate::ffi::{    
    FwpsRedirectHandleCreate0, 
    FwpsRedirectHandleDestroy0, 
    FwpsQueryConnectionRedirectState0,
    FwpsAcquireClassifyHandle0,
    FwpsReleaseClassifyHandle0,
    FwpsPendClassify0,
    FwpsAcquireWritableLayerDataPointer0,
    FwpsApplyModifiedLayerData0,
    FwpsCompleteClassify0,
    FWPS_CONNECT_REQUEST0,
    FWPS_CONNECTION_REDIRECT_STATE::* };

use super::{callout_data::CalloutData, classify::ClassifyOut};

pub struct PendRedirectResult {
    pub classify_handle: u64,       // FwpsClassifyHandle from FwpsAcquireClassifyHandle0()
    pub filter_id: u64,             // Filter ID used for the pend operation
    pub classify_out: ClassifyOut,  // ClassifyOut from FwpsPendClassify0() (DEEP COPY! The original classifyOut is on the stack)
}

// ============================================================================
// Redirect Handle Management
// ============================================================================

/// Wrapper for WFP redirect handle.
/// The redirect handle is required for local address modification in CONNECT_REDIRECT.
/// It must be created once at initialization and destroyed on cleanup.
pub struct Redirector {
    redirect_handle: *mut c_void, // from FwpsRedirectHandleCreate0
}

impl Redirector {
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
                    // layer_data is only needed here
                    if layer_data.is_null() {
                        return false;
                    }
                    let conn_req = layer_data as *const FWPS_CONNECT_REQUEST0;
                    if let Some(prev) = (*conn_req).previous_version.as_ref() {
                        if !prev.local_redirect_handle.is_null() {
                            // Connection redirected by another callout to local proxy, permitting
                            return true;                   
                        }
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

    /// Pend a redirect operation for later completion.    
    pub fn pend(&self, data: &mut CalloutData) -> Result<PendRedirectResult, i32> {
        // Acquire classify handle
        let mut classify_handle: u64 = 0;
        let status = unsafe {
            FwpsAcquireClassifyHandle0(data.classify_context, 0, &mut classify_handle)
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

        // Deep copy classify_out, NOT just store the pointer!
        // The original classifyOut is on the stack of the classifyFn callback.
        // After this function returns and the callout returns to WFP, that stack memory becomes invalid.
        let classify_out_copy = unsafe { *data.classify_out };

        return Ok(PendRedirectResult {
            classify_handle,
            filter_id: data.filter_id,
            classify_out: classify_out_copy,
        });
    }

    /// Cancel a pended redirect operation and release resources.
    pub fn cancel_pend(&self, pend_result: PendRedirectResult) {
        unsafe {                
            FwpsCompleteClassify0(
                pend_result.classify_handle,
                0,  // flags
                &pend_result.classify_out,
            );

            FwpsReleaseClassifyHandle0(pend_result.classify_handle);
        }
    }

    /// Complete a pended redirect operation.
    /// 
    /// If `new_local_ip` is Some, the connection's local address will be modified
    /// to route through the specified interface. If None, the connection proceeds
    /// without modification (permit).
    /// 
    /// This function handles:
    /// 1. Acquiring writable layer data (if redirect needed)
    /// 2. Modifying the local address
    /// 3. Applying the modifications
    /// 4. Completing the pended classify operation
    /// 5. Releasing the classify handle
    pub fn complete_redirect(
        &self,
        mut pend_result: PendRedirectResult,
        new_local_ip: Option<IpAddress>,
    ) -> Result<(), i32> {
        let result = if let Some(ref ip) = new_local_ip { 
            self.apply_redirect_modification(&mut pend_result, ip)
                .map_err(|err| { crate::err!("Failed to apply redirect modification: {:#x}", err); err})
                    // Continue to complete and release handle even on failure
        } else {
            Ok(())  // Local address not defined, no modification needed
        };
        
        unsafe {
            // Complete the pended classify operation
            FwpsCompleteClassify0(
                pend_result.classify_handle,
                0,  // flags
                &pend_result.classify_out,
            );
        
            // Set action based on result
            if result.is_ok() {
                pend_result.classify_out.action_permit();
                pend_result.classify_out.set_write_flag();
            } else {
                pend_result.classify_out.action_block();
                pend_result.classify_out.clear_write_flag();
            }        

            // Release the classify handle        
            FwpsReleaseClassifyHandle0(pend_result.classify_handle);
        }

        result
    }

    /// Apply the actual address modification to redirect the connection
    fn apply_redirect_modification(
        &self,
        pend_result: &mut PendRedirectResult,
        new_local_ip: &IpAddress,
    ) -> Result<(), i32> {
        // Acquire writable layer data pointer
        let mut writable_data: *mut c_void = core::ptr::null_mut();
        
        let status = unsafe {
            FwpsAcquireWritableLayerDataPointer0(
                pend_result.classify_handle,
                pend_result.filter_id,
                0,  // flags
                &mut writable_data,
                &mut pend_result.classify_out,
            )
        };
        if status != 0 {
            crate::err!("FwpsAcquireWritableLayerDataPointer0 failed: {:#x}", status);
            return Err(status);
        }
                
        // Modify the local address in the connection request
        let conn_req = writable_data as *mut FWPS_CONNECT_REQUEST0;
        let result = unsafe {
            // Set the new local IP address
            let ret = (*conn_req).local_address_and_port.set_ip(new_local_ip);
            match ret {
                Ok(_) => {
                    // No necessary to update local_redirect_handle, since we are not redirecting to a local proxy
                    // (*conn_req).local_redirect_handle = self.redirect_handle;
                    Ok(())
                },
                Err(e) => {
                    crate::err!("Failed to set IP address: {}", e);
                    Err(-1)
                }
            }            
        };

        // Apply the modifications
        unsafe {
            FwpsApplyModifiedLayerData0(
                pend_result.classify_handle,
                writable_data,
                0,  // flags
            );
        }

        result
    }
}

impl Drop for Redirector {
    fn drop(&mut self) {
        if !self.redirect_handle.is_null() {
            unsafe {
                FwpsRedirectHandleDestroy0(self.redirect_handle);
            }
            self.redirect_handle = core::ptr::null_mut();
        }
    }
}