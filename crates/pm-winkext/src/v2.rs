//! v2 command/info backend — port of
//! `service/firewall/interception/windowskext2` and
//! `windows_kext/kextinterface`.
//!
//! The driver exposes a byte stream: the host writes one-byte commands (some
//! followed by a packed value) and reads back framed info messages of the form
//! `[type:u8][size:u32 LE][payload]`. The framing is parsed by the shared,
//! platform-independent [`crate::protocol::parse_info`].

#[cfg(windows)]
mod windows_impl {
    #![allow(unsafe_code)]

    use core::ffi::c_void;

    use crate::protocol::{
        parse_info, simple_command, update_v4_command, update_v6_command, verdict_command, Command,
        Info, KextVerdict, Version, IOCTL_VERSION, MAX_INFO_SIZE,
    };
    use crate::sys::{self, Handle, Overlapped};
    use crate::{Error, Result};

    const ERROR_IO_PENDING: i32 = 997;
    /// Default read buffer size used by Go's `service.OpenFile(1024)`.
    pub(crate) const DEFAULT_READ_BUFFER: usize = 1024;

    /// Buffered, overlapped handle to the kernel driver device file.
    #[derive(Debug)]
    pub struct KextFile {
        handle: Handle,
        buffer: Vec<u8>,
        /// Bytes left over from the last driver read.
        pending: Vec<u8>,
    }

    impl KextFile {
        /// Wraps an already-opened device handle.
        pub(crate) fn new(handle: Handle, read_buffer_size: usize) -> Self {
            Self {
                handle,
                buffer: vec![0_u8; read_buffer_size.max(1)],
                pending: Vec::new(),
            }
        }

        /// Raw device handle, shared with the v1 backend when the driver is
        /// an old one (Go `GetKextHandle`).
        #[must_use]
        pub const fn handle(&self) -> Handle {
            self.handle
        }

        /// Closes the device handle (Go `KextFile.Close`).
        ///
        /// The handle is invalidated before returning — even when
        /// `CloseHandle` fails — so a second close cannot double-close it.
        pub fn close(&mut self) -> Result<()> {
            if sys::is_invalid(self.handle) {
                return Err(Error::KextNotReady);
            }
            let handle = self.handle;
            self.handle = sys::INVALID_HANDLE_VALUE;
            // SAFETY: `handle` was owned by this KextFile and is still open.
            sys::check(unsafe { sys::CloseHandle(handle) })
        }

        /// Reads exactly `out.len()` bytes, refilling from the driver as
        /// needed. Returns `Ok(false)` at a clean end of stream.
        fn read_exact(&mut self, out: &mut [u8]) -> Result<bool> {
            let mut filled = 0;
            while filled < out.len() {
                if self.pending.is_empty() && !self.refill()? {
                    if filled == 0 {
                        return Ok(false);
                    }
                    return Err(Error::UnexpectedRead("truncated info frame".into()));
                }
                let available = self.pending.len().min(out.len() - filled);
                out[filled..filled + available].copy_from_slice(&self.pending[..available]);
                self.pending.drain(..available);
                filled += available;
            }
            Ok(true)
        }

        /// Reads the next chunk from the driver into `pending`.
        fn refill(&mut self) -> Result<bool> {
            let mut overlapped = Overlapped::default();
            let mut count = 0_u32;
            let buffer_len = self.buffer.len() as u32;
            // SAFETY: the buffer is owned by self and stays alive for the
            // (possibly overlapped) read; the count and overlapped structs are
            // stack locals that outlive the call/wait.
            let result = unsafe {
                sys::ReadFile(
                    self.handle,
                    self.buffer.as_mut_ptr().cast::<c_void>(),
                    buffer_len,
                    &mut count,
                    &mut overlapped,
                )
            };
            if result == 0 {
                let error = std::io::Error::last_os_error();
                if error.raw_os_error() != Some(ERROR_IO_PENDING) {
                    return Err(error.into());
                }
                let mut transferred = 0_u32;
                // SAFETY: overlapped describes the read issued above.
                sys::check(unsafe {
                    sys::GetOverlappedResult(self.handle, &mut overlapped, &mut transferred, 1)
                })?;
                count = transferred;
            }
            if count == 0 {
                return Ok(false);
            }
            self.pending
                .extend_from_slice(&self.buffer[..count as usize]);
            Ok(true)
        }

        /// Writes a whole command to the driver (Go `KextFile.Write`).
        fn write_all(&self, bytes: &[u8]) -> Result<()> {
            let mut overlapped = Overlapped::default();
            let mut written = 0_u32;
            // SAFETY: `bytes` is a live slice, count and overlapped are locals
            // that outlive the call/wait.
            let result = unsafe {
                sys::WriteFile(
                    self.handle,
                    bytes.as_ptr().cast::<c_void>(),
                    bytes.len() as u32,
                    &mut written,
                    &mut overlapped,
                )
            };
            if result == 0 {
                let error = std::io::Error::last_os_error();
                if error.raw_os_error() != Some(ERROR_IO_PENDING) {
                    return Err(error.into());
                }
                let mut transferred = 0_u32;
                // SAFETY: overlapped describes the write issued above.
                sys::check(unsafe {
                    sys::GetOverlappedResult(self.handle, &mut overlapped, &mut transferred, 1)
                })?;
                written = transferred;
            }
            if written as usize != bytes.len() {
                return Err(Error::UnexpectedRead("short kext write".into()));
            }
            Ok(())
        }
    }

    /// Reads the driver version via `IOCTL_VERSION` (Go `ReadVersion`).
    pub fn read_version(file: &KextFile) -> Result<Version> {
        let mut data = [0_u8; 4];
        let mut overlapped = Overlapped::default();
        // SAFETY: `data` is a live 4-byte buffer; overlapped is a local.
        let result = unsafe {
            sys::DeviceIoControl(
                file.handle(),
                IOCTL_VERSION,
                std::ptr::null_mut(),
                0,
                data.as_mut_ptr().cast::<c_void>(),
                data.len() as u32,
                std::ptr::null_mut(),
                &mut overlapped,
            )
        };
        if result == 0 {
            let error = std::io::Error::last_os_error();
            if error.raw_os_error() != Some(ERROR_IO_PENDING) {
                return Err(error.into());
            }
            let mut transferred = 0_u32;
            // SAFETY: `overlapped` describes the ioctl issued above and
            // `file.handle()` is the same overlapped device handle.
            sys::check(unsafe {
                sys::GetOverlappedResult(file.handle(), &mut overlapped, &mut transferred, 1)
            })?;
        }
        Ok(Version::from_bytes(data))
    }

    /// Sends a command with no payload.
    pub fn send_simple(file: &KextFile, command: Command) -> Result<()> {
        file.write_all(&simple_command(command))
    }

    /// Sends a `Verdict` command.
    pub fn send_verdict(file: &KextFile, id: u64, verdict: KextVerdict) -> Result<()> {
        file.write_all(&verdict_command(id, verdict))
    }

    /// Sends an `UpdateV4` command.
    #[allow(clippy::too_many_arguments)]
    pub fn send_update_v4(
        file: &KextFile,
        protocol: u8,
        local_ip: [u8; 4],
        local_port: u16,
        remote_ip: [u8; 4],
        remote_port: u16,
        verdict: KextVerdict,
    ) -> Result<()> {
        file.write_all(&update_v4_command(
            protocol,
            local_ip,
            local_port,
            remote_ip,
            remote_port,
            verdict,
        ))
    }

    /// Sends an `UpdateV6` command.
    #[allow(clippy::too_many_arguments)]
    pub fn send_update_v6(
        file: &KextFile,
        protocol: u8,
        local_ip: [u8; 16],
        local_port: u16,
        remote_ip: [u8; 16],
        remote_port: u16,
        verdict: KextVerdict,
    ) -> Result<()> {
        file.write_all(&update_v6_command(
            protocol,
            local_ip,
            local_port,
            remote_ip,
            remote_port,
            verdict,
        ))
    }

    /// Receives the next info frame (Go `RecvInfo`).
    pub fn recv_info(file: &mut KextFile) -> Result<Option<Info>> {
        let mut header = [0_u8; 5];
        if !file.read_exact(&mut header)? {
            return Ok(None);
        }
        let size = u32::from_le_bytes([header[1], header[2], header[3], header[4]]) as usize;
        // Validate before allocating: a corrupt size field must not make us
        // reserve gigabytes (the same guard `parse_info` applies).
        if size > MAX_INFO_SIZE {
            return Err(Error::UnexpectedInfoSize);
        }
        let mut frame = Vec::with_capacity(5 + size);
        frame.extend_from_slice(&header);
        let mut payload = vec![0_u8; size];
        file.read_exact(&mut payload)?;
        frame.extend_from_slice(&payload);
        let (info, _consumed) = parse_info(&frame)?;
        Ok(Some(info))
    }
}

#[cfg(windows)]
pub(crate) use windows_impl::{
    read_version, recv_info, send_simple, send_update_v4, send_update_v6, send_verdict, KextFile,
    DEFAULT_READ_BUFFER,
};
