//! Tracks driver-owned contexts associated with UDP ALE flows.
//!
//! WFP owns each context after `FwpsFlowAssociateContext0` succeeds and returns it
//! through flowDeleteFn when the flow expires. The registration tuple is retained
//! separately so periodic lifecycle reconciliation and driver unload can
//! explicitly remove outstanding contexts without terminating their underlying
//! network flows.

use alloc::collections::BTreeMap;
use alloc::vec::Vec;
use core::cell::UnsafeCell;

#[cfg(not(test))]
use wdk::rw_spin_lock::RwSpinLock;

#[cfg(test)]
struct RwSpinLock;

#[cfg(test)]
impl RwSpinLock {
    const fn default() -> Self {
        Self
    }

    fn read_lock(&self) {}
    fn write_lock(&self) {}
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct UdpFlowRegistration {
    pub flow_context: u64,
    pub flow_id: u64,
    pub layer_id: u16,
    pub callout_id: u32,
    pub connection_instance_id: u64,
    associated: bool,
    removal_requested: bool,
}

impl UdpFlowRegistration {
    pub fn new(flow_id: u64, layer_id: u16, callout_id: u32, connection_instance_id: u64) -> Self {
        Self {
            flow_context: 0,
            flow_id,
            layer_id,
            callout_id,
            connection_instance_id,
            associated: false,
            removal_requested: false,
        }
    }
}

struct UdpFlowState {
    registrations: BTreeMap<u64, UdpFlowRegistration>,
    callbacks_in_progress: usize,
    shutting_down: bool,
}

pub struct UdpFlowCache {
    state: UnsafeCell<UdpFlowState>,
    lock: RwSpinLock,
}

// Every access to `state` is serialized by `lock`. This cache is called from
// classifyFn, flowDeleteFn, periodic cleanup and driver unload, potentially on
// different CPUs.
unsafe impl Sync for UdpFlowCache {}

impl UdpFlowCache {
    pub fn new() -> Self {
        Self {
            state: UnsafeCell::new(UdpFlowState {
                registrations: BTreeMap::new(),
                callbacks_in_progress: 0,
                shutting_down: false,
            }),
            lock: RwSpinLock::default(),
        }
    }

    /// Records a context before it is exposed to WFP.
    ///
    /// Returns false once unload has started or if the pointer/connection instance
    /// is invalid or already registered. A false result leaves ownership with the
    /// caller.
    pub fn register(&self, flow_context: u64, mut registration: UdpFlowRegistration) -> bool {
        if flow_context == 0 || registration.connection_instance_id == 0 {
            return false;
        }

        let _guard = self.lock.write_lock();
        let state = unsafe { &mut *self.state.get() };
        if state.shutting_down || state.registrations.contains_key(&flow_context) {
            return false;
        }
        registration.flow_context = flow_context;
        registration.associated = false;
        registration.removal_requested = false;
        state.registrations.insert(flow_context, registration);
        true
    }

    /// Marks a registration after WFP accepted ownership of its context. False
    /// means flowDeleteFn already claimed it while `FwpsFlowAssociateContext0` was
    /// returning, or the allocation address has since been reused.
    pub fn mark_associated(&self, flow_context: u64, connection_instance_id: u64) -> bool {
        if flow_context == 0 || connection_instance_id == 0 {
            return false;
        }

        let _guard = self.lock.write_lock();
        let state = unsafe { &mut *self.state.get() };
        let Some(registration) = state.registrations.get_mut(&flow_context) else {
            return false;
        };
        if registration.connection_instance_id != connection_instance_id {
            return false;
        }
        registration.associated = true;
        true
    }

    /// Cancels a registration after `FwpsFlowAssociateContext0` failed. WFP never
    /// owned the context in this case, so flowDeleteFn cannot race this removal.
    /// The instance check prevents an ABA pointer reuse from reclaiming a newer
    /// registration at the same allocation address.
    pub fn cancel_registration(&self, flow_context: u64, connection_instance_id: u64) -> bool {
        if flow_context == 0 || connection_instance_id == 0 {
            return false;
        }

        let _guard = self.lock.write_lock();
        let state = unsafe { &mut *self.state.get() };
        let Some(registration) = state.registrations.get(&flow_context) else {
            return false;
        };
        if registration.connection_instance_id != connection_instance_id {
            return false;
        }
        state.registrations.remove(&flow_context).is_some()
    }

    /// Claims a callback and returns whether it only needs to reclaim the context.
    ///
    /// Removing the record before doing callback work makes the record an explicit
    /// in-flight count: unload cannot drop `Device` until `finish_callback` runs.
    /// Cleanup-requested callbacks skip connection and endpoint mutation because
    /// those cache instances were already found stale. `None` means a duplicate or
    /// unknown callback whose pointer has already been reclaimed.
    /// The layer/callout check rejects a delayed callback for an older context if
    /// the allocator has reused the same address for a different registration.
    pub fn begin_callback(
        &self,
        flow_context: u64,
        layer_id: u16,
        callout_id: u32,
    ) -> Option<bool> {
        let _guard = self.lock.write_lock();
        let state = unsafe { &mut *self.state.get() };
        let registration = state.registrations.get(&flow_context)?;
        if registration.layer_id != layer_id || registration.callout_id != callout_id {
            return None;
        }
        let registration = state.registrations.remove(&flow_context)?;
        let reclaim_only = state.shutting_down || registration.removal_requested;
        state.callbacks_in_progress = state.callbacks_in_progress.saturating_add(1);
        Some(reclaim_only)
    }

    pub fn finish_callback(&self) {
        let _guard = self.lock.write_lock();
        let state = unsafe { &mut *self.state.get() };
        state.callbacks_in_progress = state.callbacks_in_progress.saturating_sub(1);
    }

    /// Returns associated contexts that periodic cleanup may consider removing.
    ///
    /// Taking this snapshot does not claim anything. The caller first compares the
    /// connection instance IDs with a later live-connection snapshot, then uses
    /// `claim_removal` to resolve races with flowDeleteFn and other cleanup paths.
    pub fn removal_candidates(&self) -> Vec<(u64, u64)> {
        let _guard = self.lock.read_lock();
        let state = unsafe { &*self.state.get() };
        state
            .registrations
            .values()
            .filter(|registration| registration.associated && !registration.removal_requested)
            .map(|registration| {
                (
                    registration.flow_context,
                    registration.connection_instance_id,
                )
            })
            .collect()
    }

    /// Atomically claims one context for `FwpsFlowRemoveContext0`. The instance
    /// check prevents a stale cleanup snapshot from claiming a newly allocated
    /// context whose pointer was reused by the allocator.
    pub fn claim_removal(
        &self,
        flow_context: u64,
        connection_instance_id: u64,
    ) -> Option<UdpFlowRegistration> {
        if flow_context == 0 || connection_instance_id == 0 {
            return None;
        }

        let _guard = self.lock.write_lock();
        let state = unsafe { &mut *self.state.get() };
        let registration = state.registrations.get_mut(&flow_context)?;
        if registration.connection_instance_id != connection_instance_id
            || !registration.associated
            || registration.removal_requested
        {
            return None;
        }
        registration.removal_requested = true;
        Some(*registration)
    }

    /// Prevents new associations before driver unload removes the outstanding ones.
    pub fn start_shutdown(&self) {
        let _guard = self.lock.write_lock();
        let state = unsafe { &mut *self.state.get() };
        state.shutting_down = true;
    }

    /// Returns the registrations still owned by WFP and marks each as requested.
    /// A record is yielded once; it remains in the map until flowDeleteFn claims it.
    pub fn pending_removals(&self) -> Vec<UdpFlowRegistration> {
        let _guard = self.lock.write_lock();
        let state = unsafe { &mut *self.state.get() };
        let mut pending = Vec::new();
        for registration in state.registrations.values_mut() {
            if registration.associated && !registration.removal_requested {
                registration.removal_requested = true;
                pending.push(*registration);
            }
        }
        pending
    }

    pub fn retry_removal(&self, flow_context: u64, connection_instance_id: u64) {
        if flow_context == 0 || connection_instance_id == 0 {
            return;
        }

        let _guard = self.lock.write_lock();
        let state = unsafe { &mut *self.state.get() };
        if let Some(registration) = state.registrations.get_mut(&flow_context) {
            if registration.connection_instance_id == connection_instance_id {
                registration.removal_requested = false;
            }
        }
    }

    #[allow(dead_code)]
    pub fn get_entries_counts(&self) -> (usize, usize) {
        let _guard = self.lock.read_lock();
        let state = unsafe { &*self.state.get() };
        (state.registrations.len(), state.callbacks_in_progress)
    }

    pub fn is_drained(&self) -> bool {
        let _guard = self.lock.read_lock();
        let state = unsafe { &*self.state.get() };
        state.registrations.is_empty() && state.callbacks_in_progress == 0
    }

    #[cfg(test)]
    pub fn snapshot(&self) -> Vec<UdpFlowRegistration> {
        let _guard = self.lock.read_lock();
        let state = unsafe { &*self.state.get() };
        state.registrations.values().copied().collect()
    }

    #[cfg(test)]
    pub fn callbacks_in_progress(&self) -> usize {
        let _guard = self.lock.read_lock();
        let state = unsafe { &*self.state.get() };
        state.callbacks_in_progress
    }
}

#[cfg(test)]
mod tests {
    use super::{UdpFlowCache, UdpFlowRegistration};

    fn registration(flow_id: u64) -> UdpFlowRegistration {
        UdpFlowRegistration::new(flow_id, 48, 7, flow_id + 1_000)
    }

    #[test]
    fn failed_association_cancels_registration() {
        let cache = UdpFlowCache::new();
        assert!(cache.register(100, registration(1)));
        assert!(cache.cancel_registration(100, 1_001));
        assert!(!cache.cancel_registration(100, 1_001));
        assert!(cache.is_drained());
    }

    #[test]
    fn stale_cancel_cannot_reclaim_reused_context() {
        let cache = UdpFlowCache::new();
        assert!(cache.register(100, registration(1)));
        assert!(!cache.cancel_registration(100, 2_002));
        assert_eq!(cache.snapshot().len(), 1);
    }

    #[test]
    fn rejects_registration_without_connection_instance() {
        let cache = UdpFlowCache::new();
        assert!(!cache.register(100, UdpFlowRegistration::new(1, 48, 7, 0)));
        assert!(cache.is_drained());
    }

    #[test]
    fn tracks_context_until_flow_delete() {
        let cache = UdpFlowCache::new();
        assert!(cache.register(100, registration(1)));
        assert!(!cache.register(100, registration(2)));
        assert!(cache.mark_associated(100, 1_001));
        assert_eq!(
            cache.snapshot(),
            alloc::vec![UdpFlowRegistration {
                flow_context: 100,
                associated: true,
                ..registration(1)
            }]
        );
        assert_eq!(cache.begin_callback(100, 48, 7), Some(false));
        assert!(!cache.is_drained());
        assert_eq!(cache.callbacks_in_progress(), 1);
        cache.finish_callback();
        assert!(cache.is_drained());
        assert!(cache.begin_callback(100, 48, 7).is_none());
    }

    #[test]
    fn periodic_removal_is_claimed_once_and_can_be_retried() {
        let cache = UdpFlowCache::new();
        assert!(cache.register(100, registration(1)));
        assert!(cache.removal_candidates().is_empty());
        assert!(cache.mark_associated(100, 1_001));

        assert_eq!(cache.removal_candidates().len(), 1);
        let claimed = cache
            .claim_removal(100, 1_001)
            .expect("candidate was not claimed");
        assert!(claimed.removal_requested);
        assert!(cache.removal_candidates().is_empty());
        assert!(cache.claim_removal(100, 1_001).is_none());
        assert!(cache.claim_removal(100, 2_002).is_none());

        cache.retry_removal(100, 1_001);
        assert_eq!(cache.removal_candidates().len(), 1);
        assert!(cache.claim_removal(100, 1_001).is_some());
        assert_eq!(cache.begin_callback(100, 48, 7), Some(true));
        cache.finish_callback();
        assert!(cache.is_drained());
    }

    #[test]
    fn shutdown_blocks_late_associations_and_drains_callback() {
        let cache = UdpFlowCache::new();
        assert!(cache.register(100, registration(1)));
        assert!(cache.mark_associated(100, 1_001));
        cache.start_shutdown();
        assert_eq!(
            cache.pending_removals(),
            alloc::vec![UdpFlowRegistration {
                flow_context: 100,
                associated: true,
                removal_requested: true,
                ..registration(1)
            }]
        );
        assert!(cache.pending_removals().is_empty());
        assert!(!cache.register(200, registration(2)));
        assert_eq!(cache.begin_callback(100, 48, 7), Some(true));
        assert!(!cache.is_drained());
        assert_eq!(cache.callbacks_in_progress(), 1);
        cache.finish_callback();
        assert!(cache.is_drained());
    }

    #[test]
    fn shutdown_waits_for_association_in_progress() {
        let cache = UdpFlowCache::new();
        assert!(cache.register(100, registration(1)));
        cache.start_shutdown();
        assert!(cache.pending_removals().is_empty());
        assert!(!cache.is_drained());

        assert!(cache.mark_associated(100, 1_001));
        assert_eq!(cache.pending_removals().len(), 1);
    }

    #[test]
    fn failed_removal_can_be_retried() {
        let cache = UdpFlowCache::new();
        assert!(cache.register(100, registration(1)));
        assert!(cache.mark_associated(100, 1_001));
        cache.start_shutdown();
        let removal = cache.pending_removals().pop().expect("pending removal");
        cache.retry_removal(removal.flow_context, removal.connection_instance_id);
        assert_eq!(cache.pending_removals().len(), 1);
    }
}
