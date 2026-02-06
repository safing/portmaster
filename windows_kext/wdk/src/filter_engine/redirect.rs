use core::ffi::c_void;
use smoltcp::wire::IpAddress;

use crate::ffi::{
    FwpsAcquireClassifyHandle0,
    FwpsReleaseClassifyHandle0,
    FwpsPendClassify0,
    FwpsAcquireWritableLayerDataPointer0,
    FwpsApplyModifiedLayerData0,
    FwpsCompleteClassify0,
    FWPS_BIND_REQUEST0,
    FWPS_CONNECT_REQUEST0};

use super::{callout_data::CalloutData, classify::ClassifyOut};

pub struct PendRedirectResult {
    pub classify_handle: u64,       // FwpsClassifyHandle from FwpsAcquireClassifyHandle0()
    pub filter_id: u64,             // Filter ID used for the pend operation
    pub classify_out: ClassifyOut,  // ClassifyOut from FwpsPendClassify0() (DEEP COPY! The original classifyOut is on the stack)
}

#[derive(Debug, Clone, Copy)]
pub enum RedirectLayer {
    BindRedirect,
    ConnectRedirect,    
}

/// Wrapper for WFP redirect operations.
/// https://learn.microsoft.com/en-us/windows-hardware/drivers/network/using-bind-or-connect-redirection
pub struct Redirector {
    // Since we only use ALE_BIND_REDIRECT layers for changing the local address on bind,
    // no need to store redirect_handle here (from FwpsRedirectHandleCreate0()).
    // 
    // But we keep this empty struct for logical grouping of redirect-related functions.
}

impl Redirector {
    /// Create a new Redirector instance.
    pub fn new() -> Result<Self, i32> {
        Ok(Self { })
    }

    /// Apply a redirect modification immediately.
    /// In general, it must be called from classify function of a BIND_REDIRECT layer callout.
    /// But can also be called from CONNECT_REDIRECT layer callout (try to avoid this if possible).
    pub fn redirect(&self, data: &mut CalloutData, new_local_ip: IpAddress, layer: RedirectLayer) -> Result<(), i32> {
        // Acquire classify handle
        let mut classify_handle: u64 = 0;
        let status = unsafe {
            FwpsAcquireClassifyHandle0(data.classify_context, 0, &mut classify_handle)
        };
        if status != 0 {
            crate::err!("FwpsAcquireClassifyHandle0 ({:?}) failed: {:#x}", layer, status);
            return Err(status);
        }

        let result = unsafe {
            self.apply_redirect_modification(
                classify_handle,
                data.filter_id,
                &mut *data.classify_out,
                &new_local_ip,
                layer
                )
                .map_err(|err| { crate::err!("Failed to apply {:?} redirect modification: {:#x}", layer, err); err})
        };

        // Set action based on result
        unsafe {
            if result.is_ok() {
                (*data.classify_out).action_permit();
                (*data.classify_out).set_write_flag();
            } else {
                (*data.classify_out).action_block();
                (*data.classify_out).clear_write_flag();
            }
        }
        
        // Release the classify handle        
        unsafe {
            FwpsReleaseClassifyHandle0(classify_handle);
        }
        return result;
    }

    /// Pend a redirect operation for later completion.
    /// Must be called from classify function of a BIND_REDIRECT layer callout. 
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
    /// Must be called from classify function of a BIND_REDIRECT layer callout to cancel the pend.
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

    /// Complete a pended bind redirect operation, initiated by `pend()`.
    /// Must be called from asynchronous function (e.g. user-mode response handler).
    /// 
    /// If `new_local_ip` is Some, the socket's local address will be modified
    /// to bind to the specified interface. If None, the bind proceeds without modification.
    pub fn complete_pend(
        &self,
        mut pend_result: PendRedirectResult,
        new_local_ip: Option<IpAddress>,
    ) -> Result<(), i32> {
        let result = if let Some(ref ip) = new_local_ip { 
            self.apply_redirect_modification(
                pend_result.classify_handle,
                pend_result.filter_id,
                &mut pend_result.classify_out,
                ip,
                RedirectLayer::BindRedirect
            )
                .map_err(|err| { crate::err!("Failed to apply bind redirect modification: {:#x}", err); err})
        } else {
            Ok(())  // No modification needed
        };
        
        unsafe {
            // Set action based on result BEFORE completing classify
            if result.is_ok() {
                pend_result.classify_out.action_permit();
                pend_result.classify_out.set_write_flag();
            } else {
                pend_result.classify_out.action_block();
                pend_result.classify_out.clear_write_flag();
            }

            // Complete the pended classify operation
            FwpsCompleteClassify0(
                pend_result.classify_handle,
                0,  // flags
                &pend_result.classify_out,
            );

            // Release the classify handle        
            FwpsReleaseClassifyHandle0(pend_result.classify_handle);
        }

        result
    }

    /// Apply the actual address modification for bind redirect (BIND_REDIRECT layer)
    fn apply_redirect_modification(
        &self,
        classify_handle: u64,           // FwpsClassifyHandle from FwpsAcquireClassifyHandle0()
        filter_id: u64,                 // Filter ID 
        classify_out: &mut ClassifyOut, // ClassifyOut from FwpsPendClassify0()
        new_local_ip: &IpAddress,       // New local IP address to set
        layer: RedirectLayer
    ) -> Result<(), i32> {        
        // Acquire writable layer data pointer
        let mut writable_data: *mut c_void = core::ptr::null_mut();        
        let status = unsafe {
            FwpsAcquireWritableLayerDataPointer0(
                classify_handle,
                filter_id,
                0,  // flags
                &mut writable_data,
                classify_out,
            )
        };
        if status != 0 {
            crate::err!("FwpsAcquireWritableLayerDataPointer0 ({:?}) failed: {:#x}", layer, status);
            return Err(status);
        }
                
        let result = unsafe {
            match layer {
                RedirectLayer::BindRedirect => {
                    // Modify the local address in the bind request
                    let bind_req = writable_data as *mut FWPS_BIND_REQUEST0;
                    
                    // Set the new local IP address
                    let ret = (*bind_req).local_address_and_port.set_ip(new_local_ip);
                    match ret {
                        Ok(_) => {
                            crate::dbg!("Bind redirect: set local IP to {:?}", new_local_ip);
                            Ok(())
                        },
                        Err(e) => {
                            crate::err!("(bind) Failed to set local IP address : {}", e);
                            Err(-1)
                        }
                    }
                }
                RedirectLayer::ConnectRedirect => {
                    let conn_req = writable_data as *mut FWPS_CONNECT_REQUEST0;
                    
                    // Set the new local IP address
                    let ret = (*conn_req).local_address_and_port.set_ip(new_local_ip);
                    match ret {
                        Ok(_) => {
                            // No necessary to update local_redirect_handle, since we are not redirecting to a local proxy
                            // (*conn_req).local_redirect_handle = self.redirect_handle;
                            Ok(())
                        },
                        Err(e) => {
                            crate::err!("(connect) Failed to set local IP address: {}", e);
                            Err(-1)
                        }
                    }
                }
            }
        };

        // Apply the modifications
        unsafe {
            FwpsApplyModifiedLayerData0(
                classify_handle,
                writable_data,
                0,  // flags
            );
        }

        result
    }
}

impl Drop for Redirector {
    fn drop(&mut self) {
        // Nothing to destroy
    }
}