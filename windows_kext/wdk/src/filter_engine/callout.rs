use super::{callout_data::CalloutData, ffi, layer::Layer};
use crate::ffi::FwpsCalloutClassifyFn;
use alloc::{borrow::ToOwned, format, string::String};
use windows_sys::{Wdk::Foundation::DEVICE_OBJECT, Win32::Foundation::HANDLE};

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
        }
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

    pub(crate) fn register_callout(
        &mut self,
        filter_engine_handle: HANDLE,
        device_object: *mut DEVICE_OBJECT,
        callout_fn: FwpsCalloutClassifyFn,
    ) -> Result<(), String> {
        match ffi::register_callout(
            device_object,
            filter_engine_handle,
            &self.name,
            &self.description,
            self.guid,
            self.layer,
            callout_fn,
        ) {
            Ok(id) => {
                self.registered = true;
                self.id = id;
            }
            Err(code) => {
                return Err(format!("failed to register callout: {}", code));
            }
        };
        return Ok(());
    }
}
