#![allow(dead_code)]

use windows_sys::Win32::NetworkManagement::WindowsFilteringPlatform::FWPS_CLASSIFY_OUT_FLAG_ABSORB;

const FWP_ACTION_FLAG_TERMINATING: u32 = 0x00001000;
const FWP_ACTION_FLAG_NON_TERMINATING: u32 = 0x00002000;
const FWP_ACTION_FLAG_CALLOUT: u32 = 0x00004000;

const FWP_ACTION_BLOCK: u32 = 0x00000001 | FWP_ACTION_FLAG_TERMINATING;
const FWP_ACTION_PERMIT: u32 = 0x00000002 | FWP_ACTION_FLAG_TERMINATING;
const FWP_ACTION_CALLOUT_TERMINATING: u32 =
    0x00000003 | FWP_ACTION_FLAG_CALLOUT | FWP_ACTION_FLAG_TERMINATING;
const FWP_ACTION_CALLOUT_INSPECTION: u32 =
    0x00000004 | FWP_ACTION_FLAG_CALLOUT | FWP_ACTION_FLAG_NON_TERMINATING;
const FWP_ACTION_CALLOUT_UNKNOWN: u32 = 0x00000005 | FWP_ACTION_FLAG_CALLOUT;
const FWP_ACTION_CONTINUE: u32 = 0x00000006 | FWP_ACTION_FLAG_NON_TERMINATING;
const FWP_ACTION_NONE: u32 = 0x00000007;
const FWP_ACTION_NONE_NO_MATCH: u32 = 0x00000008;

const FWP_CONDITION_FLAG_IS_LOOPBACK: u32 = 0x00000001;
const FWP_CONDITION_FLAG_IS_IPSEC_SECURED: u32 = 0x00000002;
const FWP_CONDITION_FLAG_IS_REAUTHORIZE: u32 = 0x00000004;
const FWP_CONDITION_FLAG_IS_WILDCARD_BIND: u32 = 0x00000008;
const FWP_CONDITION_FLAG_IS_RAW_ENDPOINT: u32 = 0x00000010;
const FWP_CONDITION_FLAG_IS_FRAGMENT: u32 = 0x00000020;
const FWP_CONDITION_FLAG_IS_FRAGMENT_GROUP: u32 = 0x00000040;
const FWP_CONDITION_FLAG_IS_IPSEC_NATT_RECLASSIFY: u32 = 0x00000080;
const FWP_CONDITION_FLAG_REQUIRES_ALE_CLASSIFY: u32 = 0x00000100;
const FWP_CONDITION_FLAG_IS_IMPLICIT_BIND: u32 = 0x00000200;
const FWP_CONDITION_FLAG_IS_REASSEMBLED: u32 = 0x00000400;
const FWP_CONDITION_FLAG_IS_NAME_APP_SPECIFIED: u32 = 0x00004000;
const FWP_CONDITION_FLAG_IS_PROMISCUOUS: u32 = 0x00008000;
const FWP_CONDITION_FLAG_IS_AUTH_FW: u32 = 0x00010000;
const FWP_CONDITION_FLAG_IS_RECLASSIFY: u32 = 0x00020000;
const FWP_CONDITION_FLAG_IS_OUTBOUND_PASS_THRU: u32 = 0x00040000;
const FWP_CONDITION_FLAG_IS_INBOUND_PASS_THRU: u32 = 0x00080000;
const FWP_CONDITION_FLAG_IS_CONNECTION_REDIRECTED: u32 = 0x00100000;

const FWPS_RIGHT_ACTION_WRITE: u32 = 0x00000001;

#[repr(C)]
#[derive(Clone, Copy)]
pub struct ClassifyOut {
    action_type: u32,
    _out_context: u64, // System use
    _filter_id: u64,   // System use
    rights: u32,
    flags: u32,
    reserved: u32,
}

impl ClassifyOut {
    // Checks if write action flag is set. Indicates if the callout can change the action.
    pub fn can_set_action(&self) -> bool {
        self.rights & FWPS_RIGHT_ACTION_WRITE > 0
    }

    /// Set block action. Write flag should be cleared, after this.
    pub fn action_block(&mut self) {
        self.action_type = FWP_ACTION_BLOCK;
    }

    /// Set permit action.
    pub fn action_permit(&mut self) {
        self.action_type = FWP_ACTION_PERMIT;
    }

    // Set continue action.
    pub fn action_continue(&mut self) {
        self.action_type = FWP_ACTION_CONTINUE;
    }

    // Set none action.
    pub fn set_none(&mut self) {
        self.action_type = FWP_ACTION_NONE;
    }

    // Set absorb flag. This will drop the packet. Used when the packets will be reinjected in the future.
    pub fn set_absorb(&mut self) {
        self.flags |= FWPS_CLASSIFY_OUT_FLAG_ABSORB;
    }

    // Removes the absorb flag.
    pub fn clear_absorb_flag(&mut self) {
        self.flags &= !FWPS_CLASSIFY_OUT_FLAG_ABSORB;
    }

    // Clear the write flag permission. Next filter in the chain will not change the action.
    pub fn clear_write_flag(&mut self) {
        self.rights &= !FWPS_RIGHT_ACTION_WRITE;
    }
}
