use core::ffi::c_void;

use super::{callout_data::CalloutData, ffi, layer::Layer};
use crate::ffi::{FwpsCalloutClassifyFn, FwpsCalloutFlowDeleteNotifyFn};
use alloc::{borrow::ToOwned, format, string::String};
use windows_sys::Win32::Foundation::HANDLE;

pub enum FilterType {
    Resettable,
    NonResettable,
}

pub struct Callout {
    pub(crate) id: u32,
    pub(super) address: u64,
    pub(crate) name: String,
    pub(crate) description: String,
    pub(crate) guid: u128,
    pub(crate) layer: Layer,
    pub(crate) action: u32,
    pub(crate) registered: bool,
    pub(crate) filter_type: FilterType,
    pub(crate) filter_id: u64,
    pub(crate) callout_fn: fn(CalloutData),
    pub(crate) flow_delete_fn: Option<FwpsCalloutFlowDeleteNotifyFn>,
}

impl Callout {
    pub fn new(
        name: &str,
        description: &str,
        guid: u128,
        layer: Layer,
        action: u32,
        filter_type: FilterType,
        callout_fn: fn(CalloutData),
    ) -> Self {
        Self {
            id: 0,
            address: 0,
            name: name.to_owned(),
            description: description.to_owned(),
            guid,
            layer,
            action,
            registered: false,
            filter_type,
            filter_id: 0,
            callout_fn,
            flow_delete_fn: None,
        }
    }

    /// Registers a flow-deletion callback for callouts that associate contexts
    /// with WFP data flows.
    pub fn with_flow_delete_fn(mut self, flow_delete_fn: FwpsCalloutFlowDeleteNotifyFn) -> Self {
        self.flow_delete_fn = Some(flow_delete_fn);
        self
    }

    pub fn register_filter(
        &mut self,
        filter_engine_handle: HANDLE,
        sublayer_guid: u128,
    ) -> Result<(), String> {
        match ffi::register_filter(
            filter_engine_handle,
            sublayer_guid,
            &self.name,
            &self.description,
            self.guid,
            self.layer,
            self.action,
            self.address, // The address of the callout is passed as context.
        ) {
            Ok(id) => {
                self.filter_id = id;
            }
            Err(error) => {
                return Err(format!("failed to register filter: {}", error));
            }
        };

        return Ok(());
    }

    /// Registers the runtime FWPS callout and records its ID immediately.
    /// Runtime registration is not covered by the FWPM transaction.
    ///
    /// # Safety
    ///
    /// `device_object` must be the live WDM device object owned by this driver and
    /// must remain valid until the runtime callout is unregistered. The callback
    /// functions and their backing driver image must remain live over the same
    /// interval.
    pub(crate) unsafe fn register_runtime_callout(
        &mut self,
        device_object: *mut c_void,
        callout_fn: FwpsCalloutClassifyFn,
    ) -> Result<(), String> {
        // SAFETY: The caller supplies the device-object and callback lifetime
        // guarantees required by the lower-level registration wrapper.
        match unsafe {
            ffi::register_runtime_callout(device_object, self.guid, callout_fn, self.flow_delete_fn)
        } {
            Ok(id) => {
                self.registered = true;
                self.id = id;
                Ok(())
            }
            Err(code) => Err(format!("failed to register callout: {}", code)),
        }
    }

    /// Adds the FWPM callout object to the current management transaction.
    pub(crate) fn register_management_callout(
        &self,
        filter_engine_handle: HANDLE,
    ) -> Result<(), String> {
        ffi::register_management_callout(
            filter_engine_handle,
            self.guid,
            self.layer,
            &self.name,
            &self.description,
        )
        .map_err(|error| format!("failed to register management callout: {}", error))
    }
}
