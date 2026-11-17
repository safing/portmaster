//! v1 ioctl backend — port of `service/firewall/interception/windowskext`.
//!
//! All foreign calls are confined to this Windows-only module and use the raw
//! ioctl codes from [`crate::protocol`]. Other platforms compile an empty
//! module.

#[cfg(windows)]
mod windows_impl {
    #![allow(unsafe_code)]

    use core::ffi::c_void;
    use std::ptr;

    use crate::protocol::{
        ConnectionStat, VerdictInfo, VerdictRequest, VerdictUpdateInfo, IOCTL_CLEAR_CACHE,
        IOCTL_GET_CONNECTIONS_STAT, IOCTL_GET_PAYLOAD, IOCTL_RECV_VERDICT_REQ, IOCTL_SET_VERDICT,
        IOCTL_SHUTDOWN_REQUEST, IOCTL_UPDATE_VERDICT,
    };
    use crate::sys::{self, Handle, Overlapped};
    use crate::{Error, Result};

    /// Maximum number of connection statistics returned in one ioctl.
    const MAX_CONNECTION_STATS: usize = 100;
    /// `ERROR_IO_PENDING` — the overlapped operation is still in flight.
    const ERROR_IO_PENDING: i32 = 997;

    /// Sends `IOCTL_SHUTDOWN_REQUEST`.
    pub fn shutdown(handle: Handle) -> Result<()> {
        io_control(handle, IOCTL_SHUTDOWN_REQUEST, None, None)?;
        Ok(())
    }

    /// Sends `IOCTL_CLEAR_CACHE`.
    pub fn clear_cache(handle: Handle) -> Result<()> {
        io_control(handle, IOCTL_CLEAR_CACHE, None, None)?;
        Ok(())
    }

    /// Applies a verdict to a packet (Go `SetVerdict`).
    pub fn set_verdict(handle: Handle, id: u32, verdict: i8) -> Result<()> {
        if id == 0 {
            return Err(Error::NoPacketId);
        }
        let info = VerdictInfo { id, verdict };
        io_control(
            handle,
            IOCTL_SET_VERDICT,
            Some(&info.to_bytes()),
            None,
        )?;
        Ok(())
    }

    /// Updates a cached connection's verdict (Go `UpdateVerdict`).
    pub fn update_verdict(
        handle: Handle,
        protocol: u8,
        local_ip: &[u8],
        local_port: u16,
        remote_ip: &[u8],
        remote_port: u16,
        verdict: i8,
    ) -> Result<()> {
        let ipv6 = local_ip.len() == 16;
        let info = VerdictUpdateInfo {
            local_ip: ip_bytes_to_array(local_ip, ipv6),
            remote_ip: ip_bytes_to_array(remote_ip, ipv6),
            local_port,
            remote_port,
            ip_v6: u8::from(ipv6),
            protocol,
            verdict: verdict as u8,
        };
        io_control(handle, IOCTL_UPDATE_VERDICT, Some(&info.to_bytes()), None)?;
        Ok(())
    }

    /// Blocks until the next verdict request arrives. `Ok(None)` means the
    /// driver returned zero bytes.
    pub fn recv_verdict_request(handle: Handle) -> Result<Option<VerdictRequest>> {
        let mut request = VerdictRequest {
            id: 0,
            pid: 0,
            direction: 0,
            ip_v6: 0,
            protocol: 0,
            flags: 0,
            local_ip: [0; 4],
            remote_ip: [0; 4],
            local_port: 0,
            remote_port: 0,
            compartment_id: 0,
            interface_index: 0,
            sub_interface_index: 0,
            packet_size: 0,
        };
        // SAFETY: `request` is a repr(C) POD struct with no padding-dependent
        // invariants; the driver writes exactly `size_of::<VerdictRequest>()`
        // bytes into it.
        let bytes = unsafe {
            std::slice::from_raw_parts_mut(
                &mut request as *mut VerdictRequest as *mut u8,
                size_of::<VerdictRequest>(),
            )
        };
        let count = io_control(handle, IOCTL_RECV_VERDICT_REQ, None, Some(bytes))?;
        if count == 0 {
            return Ok(None);
        }
        Ok(Some(request))
    }

    /// Loads the raw packet payload for a v1 packet ID (Go `GetPayload`).
    pub fn get_payload(handle: Handle, id: u32, size: u32) -> Result<Vec<u8>> {
        if id == 0 {
            return Err(Error::NoPacketId);
        }
        let mut payload = vec![0_u8; size as usize];
        let mut request = Vec::with_capacity(8);
        request.extend_from_slice(&id.to_le_bytes());
        request.extend_from_slice(&size.to_le_bytes());

        let count = io_control(handle, IOCTL_GET_PAYLOAD, Some(&request), Some(&mut payload))?;
        if count == 0 {
            return Err(Error::Command(
                "windows kext did not return any data".into(),
            ));
        }
        payload.truncate(count as usize);
        Ok(payload)
    }

    /// Reads the per-connection bandwidth table (Go `GetConnectionsStats`).
    pub fn connection_stats(handle: Handle) -> Result<Vec<ConnectionStat>> {
        let mut stats = vec![
            ConnectionStat {
                local_ip: [0; 4],
                remote_ip: [0; 4],
                local_port: 0,
                remote_port: 0,
                received_bytes: 0,
                transmitted_bytes: 0,
                ip_v6: 0,
                protocol: 0,
            };
            MAX_CONNECTION_STATS
        ];
        let count = MAX_CONNECTION_STATS as u32;
        // SAFETY: `stats` is a contiguous array of repr(C) POD entries; the
        // driver writes whole `ConnectionStat` records into it.
        let out = unsafe {
            std::slice::from_raw_parts_mut(
                stats.as_mut_ptr() as *mut u8,
                size_of::<ConnectionStat>() * MAX_CONNECTION_STATS,
            )
        };
        let bytes = io_control(
            handle,
            IOCTL_GET_CONNECTIONS_STAT,
            Some(&count.to_le_bytes()),
            Some(out),
        )?;
        let entries = (bytes as usize) / size_of::<ConnectionStat>();
        stats.truncate(entries.min(MAX_CONNECTION_STATS));
        Ok(stats)
    }

    /// Sends an ioctl with optional input and output buffers and waits for the
    /// overlapped operation, returning the number of bytes transferred.
    fn io_control(
        handle: Handle,
        code: u32,
        input: Option<&[u8]>,
        output: Option<&mut [u8]>,
    ) -> Result<u32> {
        let mut overlapped = Overlapped::default();
        let in_ptr = input.map_or(ptr::null_mut(), |bytes| bytes.as_ptr() as *mut c_void);
        let in_size = input.map_or(0, |bytes| bytes.len() as u32);
        let out_ptr = output.as_deref().map_or(ptr::null_mut(), |bytes| {
            bytes.as_ptr() as *mut c_void
        });
        let out_size = output.as_deref().map_or(0, |bytes| bytes.len() as u32);

        // SAFETY: pointer/size pairs are either null+0 or derived from live
        // slices that outlive the call; `overlapped` is local and alive.
        let result = unsafe {
            sys::DeviceIoControl(
                handle, code, in_ptr, in_size, out_ptr, out_size, ptr::null_mut(), &mut overlapped,
            )
        };
        if result == 0 {
            let error = std::io::Error::last_os_error();
            if error.raw_os_error() != Some(ERROR_IO_PENDING) {
                return Err(error.into());
            }
        }

        let mut bytes_returned = 0_u32;
        // SAFETY: `overlapped` belongs to the completed (or pending) ioctl and
        // `handle` is the same device handle used to issue it.
        sys::check(unsafe {
            sys::GetOverlappedResult(handle, &mut overlapped, &mut bytes_returned, 1)
        })?;
        Ok(bytes_returned)
    }

    /// Reproduces Go's `ipAddressToArray`: the IP chunk is read as a native
    /// `uint32` and written back big-endian, which byte-swaps on little-endian
    /// hosts. `from_be_bytes` produces that exact in-memory layout.
    fn ip_bytes_to_array(ip: &[u8], ipv6: bool) -> [u32; 4] {
        let mut out = [0_u32; 4];
        let words = usize::from(ipv6) * 3 + 1;
        for (index, word) in out.iter_mut().enumerate().take(words) {
            let start = index * 4;
            let chunk = std::array::from_fn(|offset| {
                ip.get(start + offset).copied().unwrap_or(0)
            });
            *word = u32::from_be_bytes(chunk);
        }
        out
    }
}

#[cfg(windows)]
pub(crate) use windows_impl::{
    clear_cache, connection_stats, get_payload, recv_verdict_request, set_verdict, shutdown,
    update_verdict,
};
