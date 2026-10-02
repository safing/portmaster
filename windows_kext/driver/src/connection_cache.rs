use crate::{
    connection::{Connection, ConnectionV4, ConnectionV6, Direction, RedirectInfo, Verdict},
    connection_map::{ConnectionMap, Key},
};
use alloc::{string::String, vec::Vec};

use smoltcp::wire::{IpAddress, IpProtocol};
#[cfg(not(test))]
use wdk::rw_spin_lock::RwSpinLock;

#[cfg(test)]
struct RwSpinLock<T>(std::sync::RwLock<T>);

#[cfg(test)]
impl<T> RwSpinLock<T> {
    fn new(value: T) -> Self {
        Self(std::sync::RwLock::new(value))
    }

    fn read_lock(&self) -> std::sync::RwLockReadGuard<'_, T> {
        self.0
            .read()
            .unwrap_or_else(std::sync::PoisonError::into_inner)
    }

    fn write_lock(&self) -> std::sync::RwLockWriteGuard<'_, T> {
        self.0
            .write()
            .unwrap_or_else(std::sync::PoisonError::into_inner)
    }
}

/// Connection cache with a lock owning each map it protects.
///
/// This lets callers use the cache through a shared `Device` reference. A
/// mutable reference to either map exists only while its write guard is alive.
pub struct ConnectionCache {
    connections_v4: RwSpinLock<ConnectionMap<ConnectionV4>>,
    connections_v6: RwSpinLock<ConnectionMap<ConnectionV6>>,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct ConnectionRegistration {
    pub inserted: bool,
    pub instance_id: u64,
}

/// Result of applying a verdict to one exact live cache instance.
pub struct ConnectionUpdate {
    pub redirect_info: Option<RedirectInfo>,
}

/// Merges process attribution using the same precedence for registration and
/// later flow-established updates.
fn merge_process_id(stored: &mut u64, incoming: u64) -> bool {
    // PID 0 carries no attribution. PID 4 (System) can fill an unknown entry but
    // must not replace a concrete application PID. Any other concrete PID is the
    // most useful attribution currently available.
    if incoming != 0 && (*stored == 0 || incoming != 4) {
        *stored = incoming;
        return true;
    }
    false
}

fn connection_instance_is_outbound_v4(
    connection: &ConnectionV4,
) -> Option<(u64, bool)> {
    Some((
        connection.get_instance_id(),
        matches!(connection.get_direction(), Direction::Outbound),
    ))
}

fn connection_instance_is_outbound_v6(
    connection: &ConnectionV6,
) -> Option<(u64, bool)> {
    Some((
        connection.get_instance_id(),
        matches!(connection.get_direction(), Direction::Outbound),
    ))
}

impl ConnectionCache {
    pub fn new() -> Self {
        Self {
            connections_v4: RwSpinLock::new(ConnectionMap::new()),
            connections_v6: RwSpinLock::new(ConnectionMap::new()),
        }
    }

    /// Atomically returns the existing live connection or inserts a new one.
    ///
    /// Connection construction happens before the spin lock is acquired. The
    /// exact-tuple check and insertion then share one exclusive map guard, so two
    /// classify callbacks cannot register duplicate live entries. If another
    /// callback won the race, preserve its verdict and instance ID while merging
    /// any more useful process attribution from the rejected candidate.
    ///
    /// The returned instance ID belongs to the entry selected under that same map
    /// guard. Callers must carry it into endpoint and pending-packet state rather
    /// than looking the tuple up again after the guard has been released.
    pub fn register_connection(
        &self,
        key: &Key,
        process_id: u64,
        direction: Direction,
    ) -> Result<ConnectionRegistration, String> {
        self.register_connection_with_lifecycle(key, process_id, direction, true)
    }

    /// Registers a fallback connection for which WFP exposed no endpoint identity.
    /// Its verdict remains cacheable. Outbound fallbacks are idle-expired because no
    /// native callback can identify their exact lifetime; inbound state waits for
    /// socket closure so an already-authorized flow cannot bypass cached policy.
    pub fn register_untracked_connection(
        &self,
        key: &Key,
        process_id: u64,
        direction: Direction,
    ) -> Result<ConnectionRegistration, String> {
        self.register_connection_with_lifecycle(key, process_id, direction, false)
    }

    fn register_connection_with_lifecycle(
        &self,
        key: &Key,
        process_id: u64,
        direction: Direction,
        native_lifecycle: bool,
    ) -> Result<ConnectionRegistration, String> {
        if key.is_ipv6() {
            let connection = if native_lifecycle {
                ConnectionV6::from_key(key, process_id, direction)?
            } else {
                ConnectionV6::from_untracked_key(key, process_id, direction)?
            };
            Ok(self.register_connection_v6(connection))
        } else {
            let connection = if native_lifecycle {
                ConnectionV4::from_key(key, process_id, direction)?
            } else {
                ConnectionV4::from_untracked_key(key, process_id, direction)?
            };
            Ok(self.register_connection_v4(connection))
        }
    }

    fn register_connection_v4(&self, connection: ConnectionV4) -> ConnectionRegistration {
        let process_id = connection.process_id;
        let native_lifecycle = connection.has_native_lifecycle();
        let mut connections = self.connections_v4.write_lock();
        match connections.insert_if_absent_with(connection, |existing| {
            merge_process_id(&mut existing.process_id, process_id);
            if native_lifecycle {
                existing.mark_native_lifecycle();
            }
        }) {
            Ok(instance_id) => ConnectionRegistration {
                inserted: true,
                instance_id,
            },
            Err((_connection, instance_id)) => ConnectionRegistration {
                inserted: false,
                instance_id,
            },
        }
    }

    fn register_connection_v6(&self, connection: ConnectionV6) -> ConnectionRegistration {
        let process_id = connection.process_id;
        let native_lifecycle = connection.has_native_lifecycle();
        let mut connections = self.connections_v6.write_lock();
        match connections.insert_if_absent_with(connection, |existing| {
            merge_process_id(&mut existing.process_id, process_id);
            if native_lifecycle {
                existing.mark_native_lifecycle();
            }
        }) {
            Ok(instance_id) => ConnectionRegistration {
                inserted: true,
                instance_id,
            },
            Err((_connection, instance_id)) => ConnectionRegistration {
                inserted: false,
                instance_id,
            },
        }
    }

    /// Runs `use_instance` only while the exact cache instance is still live.
    ///
    /// Endpoint tracking holds its own read guard around this call. The nested
    /// endpoint -> connection lock order matches authorization and cleanup paths,
    /// so endpoint closure cannot consume the association between identity lookup
    /// and the live-instance check.
    pub fn with_live_connection_instance<T>(
        &self,
        key: &Key,
        instance_id: u64,
        use_instance: impl FnOnce(u64) -> Option<T>,
    ) -> Option<T> {
        if instance_id == 0 {
            return None;
        }

        if key.is_ipv6() {
            let connections = self.connections_v6.read_lock();
            if connections.has_live_instance(key, instance_id) {
                return use_instance(instance_id);
            }
        } else {
            let connections = self.connections_v4.read_lock();
            if connections.has_live_instance(key, instance_id) {
                return use_instance(instance_id);
            }
        }
        None
    }

    /// Runs `use_instance` only while the exact cache generation matching a packet
    /// key is still live.
    ///
    /// Unlike the endpoint-oriented method above, this accepts a reverse-redirect
    /// key. The map's shared guard remains held while `use_instance` publishes both
    /// the pending packet and its userspace event. A lifecycle end requires the
    /// exclusive guard, so it cannot emit END between validation and publication.
    pub fn with_live_connection_instance_matching<T, R>(
        &self,
        key: &Key,
        instance_id: u64,
        value: T,
        use_instance: impl FnOnce(u64, T) -> R,
    ) -> Result<R, T> {
        if instance_id == 0 {
            return Err(value);
        }

        if key.is_ipv6() {
            let connections = self.connections_v6.read_lock();
            if connections.has_live_instance_matching(key, instance_id) {
                return Ok(use_instance(instance_id, value));
            }
        } else {
            let connections = self.connections_v4.read_lock();
            if connections.has_live_instance_matching(key, instance_id) {
                return Ok(use_instance(instance_id, value));
            }
        }
        Err(value)
    }

    /// Updates attribution on one exact live cache instance.
    ///
    /// A flow-established callback may arrive after the tuple has been reused.
    /// When its instance was learned from endpoint state, it must not update the
    /// replacement entry selected by a tuple-only lookup.
    pub fn update_process_id_instance(&self, key: &Key, instance_id: u64, process_id: u64) -> bool {
        if process_id == 0 {
            return false;
        }

        if key.is_ipv6() {
            let mut connections = self.connections_v6.write_lock();
            if let Some(conn) = connections.get_mut_instance(key, instance_id) {
                return merge_process_id(&mut conn.process_id, process_id);
            }
        } else {
            let mut connections = self.connections_v4.write_lock();
            if let Some(conn) = connections.get_mut_instance(key, instance_id) {
                return merge_process_id(&mut conn.process_id, process_id);
            }
        }
        false
    }

    /// Returns the instance ID of the current live entry.
    pub fn get_connection_instance_id(&self, key: &Key) -> Option<u64> {
        if key.is_ipv6() {
            self.read_connection_v6(key, |connection| Some(connection.get_instance_id()))
        } else {
            self.read_connection_v4(key, |connection| Some(connection.get_instance_id()))
        }
    }

    /// Returns whether the exact live connection belongs to the outbound side.
    /// A missing generation is treated as no direction by returning `None`.
    pub fn is_outbound_connection_instance(&self, key: &Key, instance_id: u64) -> Option<bool> {
        if instance_id == 0 {
            return None;
        }

        let current = if key.is_ipv6() {
            self.read_connection_v6(key, connection_instance_is_outbound_v6)
        } else {
            self.read_connection_v4(key, connection_instance_is_outbound_v4)
        }?;
        (current.0 == instance_id).then_some(current.1)
    }

    /// Returns an exact live entry that still lacks native lifecycle identity.
    pub fn get_untracked_connection_instance_id(&self, key: &Key) -> Option<u64> {
        if key.is_ipv6() {
            let connections = self.connections_v6.read_lock();
            connections.untracked_instance_id(key)
        } else {
            let connections = self.connections_v4.read_lock();
            connections.untracked_instance_id(key)
        }
    }

    /// Promotes a fallback entry after it is successfully associated with a native
    /// endpoint or flow. An exact instance check prevents a delayed association from
    /// changing a replacement connection that reused the tuple.
    pub fn mark_connection_instance_tracked(&self, key: &Key, instance_id: u64) -> bool {
        if key.is_ipv6() {
            let mut connections = self.connections_v6.write_lock();
            connections.mark_native_lifecycle_instance(key, instance_id)
        } else {
            let mut connections = self.connections_v4.write_lock();
            connections.mark_native_lifecycle_instance(key, instance_id)
        }
    }

    pub fn update_connection(&self, key: Key, verdict: Verdict) -> Option<RedirectInfo> {
        if key.is_ipv6() {
            let mut connections = self.connections_v6.write_lock();
            if let Some(conn) = connections.get_mut(&key) {
                conn.verdict = verdict;
                return conn.redirect_info();
            }
        } else {
            let mut connections = self.connections_v4.write_lock();
            if let Some(conn) = connections.get_mut(&key) {
                conn.verdict = verdict;
                return conn.redirect_info();
            }
        }
        None
    }

    /// Applies a pending verdict only to the cache instance that queued it.
    ///
    /// An endpoint can close while user space is deciding, and the same five-tuple
    /// can then be reused. Matching the instance ID prevents that delayed verdict
    /// from changing the replacement connection's policy.
    pub fn update_connection_instance(
        &self,
        key: Key,
        instance_id: u64,
        verdict: Verdict,
    ) -> Option<ConnectionUpdate> {
        if key.is_ipv6() {
            let mut connections = self.connections_v6.write_lock();
            if let Some(conn) = connections.get_mut_instance(&key, instance_id) {
                conn.verdict = verdict;
                return Some(ConnectionUpdate {
                    redirect_info: conn.redirect_info(),
                });
            }
        } else {
            let mut connections = self.connections_v4.write_lock();
            if let Some(conn) = connections.get_mut_instance(&key, instance_id) {
                conn.verdict = verdict;
                return Some(ConnectionUpdate {
                    redirect_info: conn.redirect_info(),
                });
            }
        }
        None
    }

    /// Reads a live IPv4 connection. Ended entries are not current connection
    /// state and are ignored by ALE and update paths.
    pub fn read_connection_v4<T>(
        &self,
        key: &Key,
        process_connection: fn(&ConnectionV4) -> Option<T>,
    ) -> Option<T> {
        let connections = self.connections_v4.read_lock();
        connections.read(key, process_connection)
    }

    /// Reads a live IPv6 connection. Ended entries are not current connection
    /// state and are ignored by ALE and update paths.
    pub fn read_connection_v6<T>(
        &self,
        key: &Key,
        process_connection: fn(&ConnectionV6) -> Option<T>,
    ) -> Option<T> {
        let connections = self.connections_v6.read_lock();
        connections.read(key, process_connection)
    }

    /// Reads retained IPv4 state for an exact ended generation without refreshing it.
    pub fn read_ended_connection_v4<T>(
        &self,
        key: &Key,
        process_connection: fn(&ConnectionV4) -> Option<T>,
    ) -> Option<T> {
        let connections = self.connections_v4.read_lock();
        connections.read_ended_exact(key, process_connection)
    }

    /// IPv6 counterpart of [`Self::read_ended_connection_v4`].
    pub fn read_ended_connection_v6<T>(
        &self,
        key: &Key,
        process_connection: fn(&ConnectionV6) -> Option<T>,
    ) -> Option<T> {
        let connections = self.connections_v6.read_lock();
        connections.read_ended_exact(key, process_connection)
    }

    /// Reads IPv4 policy for one packet indication. Inbound lookups use live
    /// state only so a retained tuple cannot shadow a new flow before ALE
    /// authorization; outbound lookups may use ended history for packets already
    /// in flight after closure.
    pub fn read_connection_v4_for_packet<T>(
        &self,
        key: &Key,
        packet_direction: Direction,
        process_connection: fn(&ConnectionV4) -> Option<T>,
    ) -> Option<T> {
        let connections = self.connections_v4.read_lock();
        connections.read_for_packet(key, packet_direction, process_connection)
    }

    /// IPv6 counterpart of [`Self::read_connection_v4_for_packet`].
    pub fn read_connection_v6_for_packet<T>(
        &self,
        key: &Key,
        packet_direction: Direction,
        process_connection: fn(&ConnectionV6) -> Option<T>,
    ) -> Option<T> {
        let connections = self.connections_v6.read_lock();
        connections.read_for_packet(key, packet_direction, process_connection)
    }

    /// Ends several exact IPv4 connection generations under one map write lock.
    pub fn end_connection_instances_v4(&self, instances: &[(Key, u64)]) -> Vec<ConnectionV4> {
        let mut connections = self.connections_v4.write_lock();
        connections.end_instances(instances)
    }

    /// Ends several exact IPv6 connection generations under one map write lock.
    pub fn end_connection_instances_v6(&self, instances: &[(Key, u64)]) -> Vec<ConnectionV6> {
        let mut connections = self.connections_v6.write_lock();
        connections.end_instances(instances)
    }

    pub fn end_connection_instance_v4(&self, key: Key, instance_id: u64) -> Option<ConnectionV4> {
        let mut connections = self.connections_v4.write_lock();
        connections.end_instance(key, instance_id)
    }

    pub fn end_connection_instance_v6(&self, key: Key, instance_id: u64) -> Option<ConnectionV6> {
        let mut connections = self.connections_v6.write_lock();
        connections.end_instance(key, instance_id)
    }

    /// Ends one IPv4 generation unless `defer_end` installed asynchronous
    /// lifecycle state. The decision and end share the map's write guard, which
    /// excludes packet publication for this address family.
    pub fn end_connection_instance_v4_unless(
        &self,
        key: Key,
        instance_id: u64,
        defer_end: impl FnOnce() -> Result<bool, String>,
    ) -> Result<Option<ConnectionV4>, String> {
        let mut connections = self.connections_v4.write_lock();
        if !connections.has_live_instance(&key, instance_id) {
            return Ok(None);
        }
        if defer_end()? {
            return Ok(None);
        }
        Ok(connections.end_instance(key, instance_id))
    }

    /// IPv6 counterpart of [`Self::end_connection_instance_v4_unless`].
    pub fn end_connection_instance_v6_unless(
        &self,
        key: Key,
        instance_id: u64,
        defer_end: impl FnOnce() -> Result<bool, String>,
    ) -> Result<Option<ConnectionV6>, String> {
        let mut connections = self.connections_v6.write_lock();
        if !connections.has_live_instance(&key, instance_id) {
            return Ok(None);
        }
        if defer_end()? {
            return Ok(None);
        }
        Ok(connections.end_instance(key, instance_id))
    }

    /// Runs `prepare_end` and marks the exact IPv4 generation ended under one
    /// connection-map write guard. Pending-request publication holds the matching
    /// read guard, so the prepared closure state cannot be removed before a newly
    /// published packet has either joined it or observed the ended generation.
    pub fn end_connection_instance_v4_after<T>(
        &self,
        key: Key,
        instance_id: u64,
        prepare_end: impl FnOnce() -> Option<T>,
    ) -> Option<(Option<ConnectionV4>, T)> {
        let mut connections = self.connections_v4.write_lock();
        let prepared = prepare_end()?;
        let connection = connections.end_instance(key, instance_id);
        Some((connection, prepared))
    }

    /// IPv6 counterpart of [`Self::end_connection_instance_v4_after`].
    pub fn end_connection_instance_v6_after<T>(
        &self,
        key: Key,
        instance_id: u64,
        prepare_end: impl FnOnce() -> Option<T>,
    ) -> Option<(Option<ConnectionV6>, T)> {
        let mut connections = self.connections_v6.write_lock();
        let prepared = prepare_end()?;
        let connection = connections.end_instance(key, instance_id);
        Some((connection, prepared))
    }

    pub fn end_all_on_endpoint_v4(
        &self,
        key: (IpProtocol, u16),
        local_address: Option<IpAddress>,
        process_id: Option<u64>,
    ) -> Option<Vec<ConnectionV4>> {
        let mut connections = self.connections_v4.write_lock();
        connections.end_all_on_endpoint(key, local_address, process_id)
    }

    pub fn end_all_on_endpoint_v6(
        &self,
        key: (IpProtocol, u16),
        local_address: Option<IpAddress>,
        process_id: Option<u64>,
    ) -> Option<Vec<ConnectionV6>> {
        let mut connections = self.connections_v6.write_lock();
        connections.end_all_on_endpoint(key, local_address, process_id)
    }

    pub fn end_untracked_on_endpoint_v4(
        &self,
        key: (IpProtocol, u16),
        local_address: Option<IpAddress>,
        process_id: Option<u64>,
    ) -> Option<Vec<ConnectionV4>> {
        let mut connections = self.connections_v4.write_lock();
        connections.end_untracked_on_endpoint(key, local_address, process_id)
    }

    pub fn end_untracked_on_endpoint_v6(
        &self,
        key: (IpProtocol, u16),
        local_address: Option<IpAddress>,
        process_id: Option<u64>,
    ) -> Option<Vec<ConnectionV6>> {
        let mut connections = self.connections_v6.write_lock();
        connections.end_untracked_on_endpoint(key, local_address, process_id)
    }

    /// Ends inactive outbound entries that have no native endpoint or flow callback.
    pub fn end_inactive_untracked_connections(&self) -> (Vec<ConnectionV4>, Vec<ConnectionV6>) {
        let v4 = {
            let mut connections = self.connections_v4.write_lock();
            connections.end_inactive_untracked_connections()
        };
        let v6 = {
            let mut connections = self.connections_v6.write_lock();
            connections.end_inactive_untracked_connections()
        };
        (v4, v6)
    }

    /// Removes retained ended history after its late-packet grace period.
    pub fn clean_ended_connections(&self) {
        {
            let mut connections = self.connections_v4.write_lock();
            connections.clean_ended_connections();
        }
        {
            let mut connections = self.connections_v6.write_lock();
            connections.clean_ended_connections();
        }
    }

    /// Returns a sorted snapshot of every live UDP connection-cache instance ID
    /// without refreshing connection activity.
    pub fn live_udp_instance_ids(&self) -> Vec<u64> {
        let mut instance_ids = Vec::new();
        {
            let connections = self.connections_v4.read_lock();
            connections.append_live_udp_instance_ids(&mut instance_ids);
        }
        {
            let connections = self.connections_v6.read_lock();
            connections.append_live_udp_instance_ids(&mut instance_ids);
        }
        instance_ids.sort_unstable();
        instance_ids
    }

    pub fn clear(&self) {
        {
            let mut connections = self.connections_v4.write_lock();
            connections.clear();
        }
        {
            let mut connections = self.connections_v6.write_lock();
            connections.clear();
        }
    }

    #[allow(dead_code)]
    pub fn get_entries_counts(&self) -> (usize, usize) {
        let v4 = {
            let connections = self.connections_v4.read_lock();
            connections.get_count()
        };
        let v6 = {
            let connections = self.connections_v6.read_lock();
            connections.get_count()
        };

        (v4, v6)
    }

    #[allow(dead_code)]
    pub fn get_untracked_entries_counts(&self) -> (usize, usize) {
        let v4 = {
            let connections = self.connections_v4.read_lock();
            connections.get_untracked_count()
        };
        let v6 = {
            let connections = self.connections_v6.read_lock();
            connections.get_untracked_count()
        };

        (v4, v6)
    }
}

#[cfg(test)]
mod tests {
    use super::ConnectionCache;
    use crate::{
        connection::{Direction, Verdict, PM_DNS_PORT},
        connection_map::Key,
    };
    use smoltcp::wire::{IpAddress, IpProtocol, Ipv4Address};
    use std::sync::TryLockError;

    fn key(remote_address: [u8; 4], remote_port: u16) -> Key {
        Key {
            protocol: IpProtocol::Tcp,
            local_address: IpAddress::Ipv4(Ipv4Address::new(10, 0, 0, 1)),
            local_port: 50_000,
            remote_address: IpAddress::Ipv4(Ipv4Address::from_bytes(&remote_address)),
            remote_port,
        }
    }

    #[test]
    fn native_registration_promotes_existing_fallback_instance() {
        let cache = ConnectionCache::new();
        let tuple = key([192, 0, 2, 10], 443);
        let fallback = cache
            .register_untracked_connection(&tuple, 0, Direction::Outbound)
            .expect("fallback registration");
        assert!(fallback.inserted);

        let tracked = cache
            .register_connection(&tuple, 100, Direction::Outbound)
            .expect("native registration");
        assert!(!tracked.inserted);
        assert_eq!(tracked.instance_id, fallback.instance_id);
        let (expired_v4, expired_v6) = cache.end_inactive_untracked_connections();
        assert!(expired_v4.is_empty());
        assert!(expired_v6.is_empty());
        assert_eq!(
            cache.read_connection_v4(&tuple, |connection| Some(connection.process_id)),
            Some(100)
        );
    }

    #[test]
    fn publication_callback_holds_liveness_guard_until_it_returns() {
        let cache = ConnectionCache::new();
        let tuple = key([192, 0, 2, 1], 443);
        let registration = cache
            .register_connection(&tuple, 100, Direction::Outbound)
            .expect("connection registration");

        let result = cache.with_live_connection_instance_matching(
            &tuple,
            registration.instance_id,
            41,
            |_, value| {
                // A lifecycle end needs this map's exclusive guard. Verify that the
                // publication callback still owns the shared guard, rather than
                // merely running after an already-stale liveness check.
                assert!(matches!(
                    cache.connections_v4.0.try_write(),
                    Err(TryLockError::WouldBlock)
                ));
                value + 1
            },
        );
        assert_eq!(result, Ok(42));

        assert!(cache
            .end_connection_instance_v4(tuple, registration.instance_id)
            .is_some());
        let mut callback_ran = false;
        let result = cache.with_live_connection_instance_matching(
            &tuple,
            registration.instance_id,
            42,
            |_, value| {
                callback_ran = true;
                value
            },
        );

        assert_eq!(result, Err(42));
        assert!(!callback_ran);
    }

    #[test]
    fn publication_accepts_exact_redirect_generation_only_while_live() {
        let cache = ConnectionCache::new();
        let original = key([203, 0, 113, 1], 53);
        let redirect = key([127, 0, 0, 1], PM_DNS_PORT);
        let registration = cache
            .register_connection(&original, 100, Direction::Outbound)
            .expect("connection registration");
        assert!(cache
            .update_connection_instance(
                original,
                registration.instance_id,
                Verdict::RedirectNameServer,
            )
            .is_some());

        assert_eq!(
            cache.with_live_connection_instance_matching(
                &redirect,
                registration.instance_id,
                41,
                |_, value| value + 1,
            ),
            Ok(42)
        );
        assert_eq!(
            cache.with_live_connection_instance_matching(
                &redirect,
                registration.instance_id.wrapping_add(1),
                41,
                |_, value| value + 1,
            ),
            Err(41)
        );

        assert!(cache
            .end_connection_instance_v4(original, registration.instance_id)
            .is_some());
        assert_eq!(
            cache.with_live_connection_instance_matching(
                &redirect,
                registration.instance_id,
                41,
                |_, value| value + 1,
            ),
            Err(41)
        );
    }

    #[test]
    fn endpoint_closure_decision_is_atomic_with_publication_guard() {
        let cache = ConnectionCache::new();
        let tuple = key([192, 0, 2, 2], 443);
        let registration = cache
            .register_connection(&tuple, 100, Direction::Outbound)
            .expect("connection registration");

        let ended = cache
            .end_connection_instance_v4_unless(tuple, registration.instance_id, || {
                assert!(matches!(
                    cache.connections_v4.0.try_read(),
                    Err(TryLockError::WouldBlock)
                ));
                Ok(true)
            })
            .expect("defer decision");
        assert!(ended.is_none());
        assert!(cache
            .with_live_connection_instance(&tuple, registration.instance_id, Some)
            .is_some());

        let ended = cache
            .end_connection_instance_v4_unless(tuple, registration.instance_id, || Ok(false))
            .expect("inline end");
        assert!(ended.is_some());
    }

    #[test]
    fn stale_endpoint_does_not_install_deferred_state() {
        let cache = ConnectionCache::new();
        let tuple = key([192, 0, 2, 3], 443);
        let registration = cache
            .register_connection(&tuple, 100, Direction::Outbound)
            .expect("connection registration");
        assert!(cache
            .end_connection_instance_v4(tuple, registration.instance_id)
            .is_some());

        let result = cache.end_connection_instance_v4_unless(
            tuple,
            registration.instance_id,
            || -> Result<bool, alloc::string::String> {
                panic!("stale endpoint attempted to install closure state")
            },
        );
        assert!(matches!(result, Ok(None)));
    }
}
