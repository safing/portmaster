use core::ffi::c_void;

use crate::alloc::borrow::ToOwned;
use crate::driver::Driver;
use crate::ffi::FWPS_FILTER2;
use crate::filter_engine::transaction::Transaction;
use crate::{dbg, info};
use alloc::boxed::Box;
use alloc::string::String;
use alloc::{format, vec::Vec};
use windows_sys::Wdk::Foundation::DEVICE_OBJECT;
use windows_sys::Win32::Foundation::{HANDLE, INVALID_HANDLE_VALUE};

use self::callout::{Callout, FilterType};
use self::callout_data::CalloutData;
use self::classify::ClassifyOut;
use self::layer::IncomingValues;
use self::metadata::FwpsIncomingMetadataValues;

pub mod callout;
pub mod callout_data;
pub(crate) mod classify;
#[allow(dead_code)]
pub mod ffi;
pub mod layer;
pub(crate) mod metadata;
pub mod net_buffer;
pub mod packet;
pub mod stream_data;
pub mod transaction;
// Helper functions for ALE Readirect layers. Not needed for the current implementation.
// pub mod connect_request;

pub struct FilterEngine {
    device_object: *mut DEVICE_OBJECT,
    handle: HANDLE,
    sublayer_guid: u128,
    committed: bool,
    callouts: Option<Vec<Box<Callout>>>,
}

impl FilterEngine {
    pub fn new(driver: &Driver, layer_guid: u128) -> Result<Self, String> {
        let filter_engine_handle: HANDLE;
        match ffi::create_filter_engine() {
            Ok(handle) => {
                filter_engine_handle = handle;
            }
            Err(code) => {
                return Err(format!("failed to initialize filter engine {}", code).to_owned());
            }
        }
        Ok(Self {
            device_object: driver.get_device_object(),
            handle: filter_engine_handle,
            sublayer_guid: layer_guid,
            committed: false,
            callouts: None,
        })
    }

    pub fn commit(&mut self, callouts: Vec<Callout>) -> Result<(), String> {
        {
            // Begin write transaction. This is also a lock guard.
            let mut filter_engine = match Transaction::begin_write(self) {
                Ok(transaction) => transaction,
                Err(err) => {
                    return Err(err);
                }
            };

            if let Err(err) = filter_engine.register_sublayer() {
                return Err(format!("filter_engine: {}", err));
            }

            dbg!("Callouts count: {}", callouts.len());
            let mut boxed_callouts = Vec::new();
            // Register all callouts
            for callout in callouts {
                let mut callout = Box::new(callout);
                callout.address = callout.as_ref() as *const Callout as u64;

                if let Err(err) = callout.register_callout(
                    filter_engine.handle,
                    filter_engine.device_object,
                    catch_all_callout,
                ) {
                    // This will destroy the callout structs.
                    return Err(err);
                }
                if let Err(err) =
                    callout.register_filter(filter_engine.handle, filter_engine.sublayer_guid)
                {
                    // This will destroy the callout structs.
                    return Err(err);
                }
                dbg!(
                    "registering callout: {} -> {}",
                    callout.name,
                    callout.filter_id
                );
                boxed_callouts.push(callout)
            }
            if let Some(callouts) = &mut filter_engine.callouts {
                callouts.append(&mut boxed_callouts);
            } else {
                filter_engine.callouts = Some(boxed_callouts);
            }

            filter_engine.commit()?
        }
        self.committed = true;
        info!("transaction committed");

        return Ok(());
    }

    pub fn reset_all_filters(&mut self) -> Result<(), String> {
        // Begin to write transaction. This is also a lock guard. It will abort if transaction is not committed.
        let mut filter_engine = match Transaction::begin_write(self) {
            Ok(transaction) => transaction,
            Err(err) => {
                return Err(err);
            }
        };
        let filter_engine_handle = filter_engine.handle;
        let sublayer_guid = filter_engine.sublayer_guid;
        if let Some(callouts) = &mut filter_engine.callouts {
            for callout in callouts {
                if let FilterType::Resettable = callout.filter_type {
                    if callout.filter_id != 0 {
                        // Remove old filter.
                        if let Err(err) =
                            ffi::unregister_filter(filter_engine_handle, callout.filter_id)
                        {
                            return Err(format!("filter_engine: {}", err));
                        }
                        callout.filter_id = 0;
                    }
                    // Create new filter.
                    if let Err(err) = callout.register_filter(filter_engine_handle, sublayer_guid) {
                        return Err(format!("filter_engine: {}", err));
                    }
                }
            }
        }
        // Commit transaction.
        filter_engine.commit()?;
        return Ok(());
    }

    fn register_sublayer(&self) -> Result<(), String> {
        let result = ffi::register_sublayer(
            self.handle,
            "PortmasterSublayer",
            "The Portmaster sublayer holds all it's filters.",
            self.sublayer_guid,
        );
        if let Err(code) = result {
            return Err(format!("failed to register sublayer: {}", code));
        }

        return Ok(());
    }
}

impl Drop for FilterEngine {
    fn drop(&mut self) {
        dbg!("Unregistering callouts");
        if let Some(callouts) = &self.callouts {
            for callout in callouts {
                if callout.registered {
                    if let Err(code) = ffi::unregister_callout(callout.id) {
                        dbg!("failed to unregister callout: {}", code);
                    }
                    if callout.filter_id != 0 {
                        if let Err(code) = ffi::unregister_filter(self.handle, callout.filter_id) {
                            dbg!("failed to unregister filter: {}", code)
                        }
                    }
                }
            }
        }

        if self.committed {
            if let Err(code) = ffi::unregister_sublayer(self.handle, self.sublayer_guid) {
                dbg!("Failed to unregister sublayer: {}", code);
            }
        }

        if !self.handle.is_null() && self.handle != INVALID_HANDLE_VALUE {
            _ = ffi::filter_engine_close(self.handle);
        }
    }
}

#[no_mangle]
unsafe extern "C" fn catch_all_callout(
    fixed_values: *const IncomingValues,
    meta_values: *const FwpsIncomingMetadataValues,
    layer_data: *mut c_void,
    _context: *mut c_void,
    filter: *const FWPS_FILTER2,
    _flow_context: u64,
    classify_out: *mut ClassifyOut,
) {
    let filter = &(*filter);
    // Filter context is the address of the callout.
    let callout = filter.context as *mut Callout;

    if let Some(callout) = callout.as_ref() {
        // Setup callout data.
        let array = core::slice::from_raw_parts(
            (*fixed_values).incoming_value_array,
            (*fixed_values).value_count as usize,
        );
        let data = CalloutData {
            layer: callout.layer,
            callout_id: filter.context as usize,
            values: array,
            metadata: meta_values,
            classify_out,
            layer_data,
        };
        // Call the defined function.
        (callout.callout_fn)(data);
    }
}
