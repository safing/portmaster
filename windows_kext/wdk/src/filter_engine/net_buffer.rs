use core::mem::MaybeUninit;

use alloc::{
    string::{String, ToString},
    vec::Vec,
};
use windows_sys::Wdk::System::SystemServices::{
    IoAllocateMdl, IoFreeMdl, MmBuildMdlForNonPagedPool,
};

use crate::{
    allocator::POOL_TAG,
    ffi::{
        FwpsAllocateNetBufferAndNetBufferList0, FwpsFreeNetBufferList0,
        NdisAdvanceNetBufferDataStart, NdisAllocateNetBufferListPool, NdisFreeNetBufferListPool,
        NdisGetDataBuffer, NdisRetreatNetBufferDataStart, NDIS_HANDLE, NDIS_OBJECT_TYPE_DEFAULT,
        NET_BUFFER_LIST, NET_BUFFER_LIST_POOL_PARAMETERS,
        NET_BUFFER_LIST_POOL_PARAMETERS_REVISION_1,
    },
    utils::check_ntstatus,
};

pub struct NetBufferList {
    pub(crate) nbl: *mut NET_BUFFER_LIST,
    data: Option<Vec<u8>>,
    advance_on_drop: Option<u32>,
}

/// Owned result of cloning every independent packet in one native NBL.
pub enum NetBufferListClones {
    Single(NetBufferList),
    Batch(Vec<NetBufferList>),
}

// Owned packet clones are deliberately transferred from classify callbacks to
// the verdict path and later to an asynchronous injection completion callback.
// WFP/NDIS own synchronization of the native NBL; this wrapper never shares
// mutable Rust access to it between those phases.
unsafe impl Send for NetBufferList {}

impl NetBufferList {
    /// Wraps a native net-buffer list without taking ownership of it.
    ///
    /// # Safety
    ///
    /// `nbl` may be null. Otherwise it must point to a live, properly aligned
    /// `NET_BUFFER_LIST` whose net buffers and MDLs remain valid for every use
    /// of the returned wrapper. If [`Self::retreat`] enables automatic advance,
    /// the NBL must also remain valid through this value's drop. The caller must
    /// synchronize native mutation and must not let this wrapper, or any iterator
    /// or wrapper derived from it, outlive the native NBL chain.
    pub unsafe fn new(nbl: *mut NET_BUFFER_LIST) -> NetBufferList {
        NetBufferList {
            nbl,
            data: None,
            advance_on_drop: None,
        }
    }

    /// Iterates the native NBL chain beginning at this wrapper.
    ///
    /// # Safety
    ///
    /// The chain must remain live and unmodified until the iterator and every
    /// wrapper yielded by it are no longer used. In particular, yielded wrappers
    /// must not outlive an owning `NetBufferList` that will free the chain.
    pub unsafe fn iter(&self) -> NetBufferListIter {
        // SAFETY: The caller accepts the lifetime and synchronization contract
        // above for the same native chain represented by this wrapper.
        unsafe { NetBufferListIter::new(self.nbl) }
    }

    pub fn read_bytes(&self, buffer: &mut [u8]) -> Result<(), ()> {
        unsafe {
            let Some(nbl) = self.nbl.as_ref() else {
                return Err(());
            };
            let nb = nbl.Header.first_net_buffer;
            if let Some(nb) = nb.as_ref() {
                let data_length = nb.nbSize.DataLength;
                if data_length == 0 {
                    return Err(());
                }

                if buffer.len() > data_length as usize {
                    return Err(());
                }

                let mut ptr =
                    NdisGetDataBuffer(nb, buffer.len() as u32, core::ptr::null_mut(), 1, 0);
                if !ptr.is_null() {
                    buffer.copy_from_slice(core::slice::from_raw_parts(ptr, buffer.len()));
                    return Ok(());
                }

                ptr = NdisGetDataBuffer(nb, buffer.len() as u32, buffer.as_mut_ptr(), 1, 0);
                if !ptr.is_null() {
                    return Ok(());
                }
            }
        }
        return Err(());
    }

    /// Copies as much of the first net buffer as fits in `buffer` and returns the
    /// copied length. Unlike `get_data_length`, this never sums independent net
    /// buffers in a batched NBL. The common path needs one NDIS read; if a full
    /// prefix cannot be materialized, `fallback_length` preserves a smaller parse.
    pub fn read_bytes_up_to(&self, buffer: &mut [u8], fallback_length: usize) -> Result<usize, ()> {
        if buffer.is_empty() {
            return Ok(0);
        }

        unsafe {
            let Some(nbl) = self.nbl.as_ref() else {
                return Err(());
            };
            let Some(nb) = nbl.Header.first_net_buffer.as_ref() else {
                return Err(());
            };
            let length = core::cmp::min(nb.nbSize.DataLength as usize, buffer.len());
            if length == 0 {
                return Err(());
            }

            let storage = buffer.as_mut_ptr();
            let ptr = NdisGetDataBuffer(nb, length as u32, storage, 1, 0);
            if !ptr.is_null() {
                if ptr != storage {
                    buffer[..length].copy_from_slice(core::slice::from_raw_parts(ptr, length));
                }
                return Ok(length);
            }

            let fallback_length = core::cmp::min(length, fallback_length);
            if fallback_length == 0 || fallback_length == length {
                return Err(());
            }
            let ptr = NdisGetDataBuffer(nb, fallback_length as u32, storage, 1, 0);
            if ptr.is_null() {
                return Err(());
            }
            if ptr != storage {
                buffer[..fallback_length]
                    .copy_from_slice(core::slice::from_raw_parts(ptr, fallback_length));
            }
            Ok(fallback_length)
        }
    }

    pub fn clone(&self, net_allocator: &NetworkAllocator) -> Result<NetBufferList, String> {
        unsafe {
            let Some(nbl) = self.nbl.as_ref() else {
                return Err("net buffer list is null".to_string());
            };

            let nb = nbl.Header.first_net_buffer;
            if let Some(nb) = nb.as_ref() {
                let data_length = nb.nbSize.DataLength;
                if data_length == 0 {
                    return Err("can't clone empty packet".to_string());
                }

                // Allocate space in buffer, if buffer is too small.
                let mut buffer = alloc::vec![0_u8; data_length as usize];

                let buffer_ptr = buffer.as_mut_ptr();

                // Two options returns a pointer to the raw packet buffer,
                // or copies the data to the supplied buffer
                // and returns a pointer to the supplied buffer.
                let ptr = NdisGetDataBuffer(nb, data_length, buffer_ptr, 1, 0);

                if ptr.is_null() {
                    return Err("failed to copy packet buffer".to_string());
                }

                // If the pointers differ the data is not in the correct place.
                if ptr != buffer_ptr {
                    buffer.copy_from_slice(core::slice::from_raw_parts(ptr, data_length as usize));
                }

                // SAFETY: The global allocator returns nonpaged storage, and
                // `buffer` is moved into the wrapper without changing its backing
                // allocation. The wrapper frees the NBL before dropping the Vec.
                let new_nbl = net_allocator.wrap_packet_in_nbl(&mut buffer)?;

                return Ok(NetBufferList {
                    nbl: new_nbl,
                    data: Some(buffer),
                    advance_on_drop: None,
                });
            } else {
                return Err("net buffer is null".to_string());
            }
        }
    }

    /// Clones every NET_BUFFER in this NBL into an independent packet.
    ///
    /// WFP may batch multiple packets in one NET_BUFFER_LIST. Each returned
    /// NBL owns its packet data and can be injected independently.
    pub fn clone_packets(
        &self,
        net_allocator: &NetworkAllocator,
    ) -> Result<NetBufferListClones, String> {
        unsafe {
            let Some(nbl) = self.nbl.as_ref() else {
                return Err("net buffer list is null".to_string());
            };

            let mut packet_count = 0;
            let mut nb = nbl.Header.first_net_buffer;
            while let Some(nb_ref) = nb.as_ref() {
                packet_count += 1;
                nb = nb_ref.Next;
            }
            if packet_count == 0 {
                return Err("net buffer list has no packets".to_string());
            }
            if packet_count == 1 {
                return self.clone(net_allocator).map(NetBufferListClones::Single);
            }

            // Batched sends can contain dozens of packets. Reserve their exact count
            // so the owning vector never has to grow while cloning at DISPATCH_LEVEL.
            let mut packets = Vec::with_capacity(packet_count);
            nb = nbl.Header.first_net_buffer;
            while let Some(nb_ref) = nb.as_ref() {
                let data_length = nb_ref.nbSize.DataLength;
                if data_length == 0 {
                    return Err("can't clone empty packet".to_string());
                }

                let mut buffer = alloc::vec![0_u8; data_length as usize];
                let buffer_ptr = buffer.as_mut_ptr();
                let ptr = NdisGetDataBuffer(nb, data_length, buffer_ptr, 1, 0);
                if ptr.is_null() {
                    return Err("failed to copy packet buffer".to_string());
                }
                if ptr != buffer_ptr {
                    buffer.copy_from_slice(core::slice::from_raw_parts(ptr, data_length as usize));
                }

                // SAFETY: The global allocator returns nonpaged storage, and
                // `buffer` is moved into the wrapper without changing its backing
                // allocation. The wrapper frees the NBL before dropping the Vec.
                let new_nbl = net_allocator.wrap_packet_in_nbl(&mut buffer)?;
                packets.push(NetBufferList {
                    nbl: new_nbl,
                    data: Some(buffer),
                    advance_on_drop: None,
                });
                nb = nb_ref.Next;
            }

            Ok(NetBufferListClones::Batch(packets))
        }
    }
    pub fn get_data_mut(&mut self) -> Option<&mut [u8]> {
        if let Some(data) = &mut self.data {
            return Some(data.as_mut_slice());
        }
        return None;
    }

    pub fn get_data(&self) -> Option<&[u8]> {
        if let Some(data) = &self.data {
            return Some(data.as_slice());
        }
        return None;
    }

    pub fn get_data_length(&self) -> u32 {
        unsafe {
            if let Some(nbl) = self.nbl.as_ref() {
                let mut nb = nbl.Header.first_net_buffer;
                let mut data_length = 0;
                while !nb.is_null() {
                    let mut next = core::ptr::null_mut();
                    if let Some(nb) = nb.as_ref() {
                        data_length += nb.nbSize.DataLength;
                        next = nb.Next;
                    }
                    nb = next;
                }

                data_length
            } else {
                0
            }
        }
    }

    /// Sums the data length of every net buffer in the list, excluding
    /// `header_len` leading bytes of each one.
    ///
    /// The header is subtracted per net buffer, not once for the whole list,
    /// because a single net buffer list may carry several independent packets
    /// (for example batched datagram sends), each with its own header.
    pub fn get_data_length_excluding_header(&self, header_len: u32) -> usize {
        unsafe {
            let Some(nbl) = self.nbl.as_ref() else {
                return 0;
            };

            let mut nb = nbl.Header.first_net_buffer;
            let mut length: usize = 0;
            while !nb.is_null() {
                let mut next = core::ptr::null_mut();
                if let Some(buffer) = nb.as_ref() {
                    // Saturating: a net buffer shorter than the header would be
                    // malformed, never report a negative payload for it.
                    length += buffer.nbSize.DataLength.saturating_sub(header_len) as usize;
                    next = buffer.Next;
                }
                nb = next;
            }

            length
        }
    }

    /// Retreats the start of the first net buffer.
    ///
    /// When `auto_advance` is true, the original offset is restored when this
    /// wrapper is dropped.
    pub fn retreat(&mut self, size: u32, auto_advance: bool) -> Result<(), String> {
        unsafe {
            let Some(nbl) = self.nbl.as_mut() else {
                return Err("net buffer list is null".to_string());
            };
            let Some(nb) = nbl.Header.first_net_buffer.as_mut() else {
                return Err("net buffer is null".to_string());
            };

            let status = NdisRetreatNetBufferDataStart(nb as _, size, 0, core::ptr::null_mut());
            check_ntstatus(status)?;
            if auto_advance {
                self.advance_on_drop = Some(size);
            }
            Ok(())
        }
    }

    /// Advances the MDL of the buffer.
    pub fn advance(&mut self, size: u32) {
        unsafe {
            if let Some(nbl) = self.nbl.as_mut() {
                if let Some(nb) = nbl.Header.first_net_buffer.as_mut() {
                    NdisAdvanceNetBufferDataStart(nb as _, size, 0, core::ptr::null_mut());
                }
            }
        }
    }
}

impl Drop for NetBufferList {
    fn drop(&mut self) {
        if let Some(advance_amount) = self.advance_on_drop {
            self.advance(advance_amount);
        }
        if self.data.is_some() {
            // SAFETY: `data` is set only by `clone` and `clone_packets`, which pair
            // this NBL with the backing allocation used to build its MDL. This
            // wrapper has exclusive ownership and is consuming that pair now.
            unsafe { NetworkAllocator::free_net_buffer(self.nbl) };
        }
    }
}

pub struct NetBufferListIter(*mut NET_BUFFER_LIST);

impl NetBufferListIter {
    /// Creates an iterator over a native NBL chain.
    ///
    /// # Safety
    ///
    /// `nbl` may be null. Otherwise it and every non-null pointer reachable
    /// through `NET_BUFFER_LIST::Header.next` must identify a live, properly
    /// aligned NBL. The chain must remain stable until this iterator and every
    /// `NetBufferList` it yields are no longer used, and the caller must
    /// synchronize any native mutation for that entire interval.
    pub unsafe fn new(nbl: *mut NET_BUFFER_LIST) -> Self {
        Self(nbl)
    }
}

impl Iterator for NetBufferListIter {
    type Item = NetBufferList;

    fn next(&mut self) -> Option<Self::Item> {
        unsafe {
            if let Some(nbl) = self.0.as_mut() {
                self.0 = nbl.Header.next as _;
                return Some(NetBufferList {
                    nbl,
                    data: None,
                    advance_on_drop: None,
                });
            }
            None
        }
    }
}

/// Copies a prefix from the first net buffer in a native NBL.
///
/// # Safety
///
/// `nbl` may be null. Otherwise it must point to a live, properly aligned
/// `NET_BUFFER_LIST` whose first net buffer and backing MDLs remain valid and
/// readable for the duration of this call. The caller must prevent concurrent
/// mutation that would invalidate those objects while NDIS reads them.
pub unsafe fn read_packet_partial(nbl: *mut NET_BUFFER_LIST, buffer: &mut [u8]) -> Result<(), ()> {
    unsafe {
        let Some(nbl) = nbl.as_ref() else {
            return Err(());
        };
        let nb = nbl.Header.first_net_buffer;
        if let Some(nb) = nb.as_ref() {
            let data_length = nb.nbSize.DataLength;
            if data_length == 0 {
                return Err(());
            }

            if buffer.len() > data_length as usize {
                return Err(());
            }

            let storage = buffer.as_mut_ptr();
            let ptr = NdisGetDataBuffer(nb, buffer.len() as u32, storage, 1, 0);
            if ptr.is_null() {
                return Err(());
            }

            // When the requested bytes are already contiguous, NDIS returns a
            // pointer into the original NET_BUFFER and does not populate Storage.
            // Copy that range explicitly so this helper always fulfils its contract
            // of writing the caller-provided buffer.
            if ptr != storage {
                buffer.copy_from_slice(core::slice::from_raw_parts(ptr, buffer.len()));
            }
            return Ok(());
        }
    }
    return Err(());
}

pub struct NetworkAllocator {
    pool_handle: NDIS_HANDLE,
}

// NDIS NBL pools support concurrent allocation/free operations. The handle is
// immutable until Device teardown, which starts only after dispatch and classify
// admission have drained.
unsafe impl Send for NetworkAllocator {}
unsafe impl Sync for NetworkAllocator {}

impl NetworkAllocator {
    pub fn new() -> Result<Self, String> {
        unsafe {
            let mut params: NET_BUFFER_LIST_POOL_PARAMETERS = MaybeUninit::zeroed().assume_init();
            params.Header.Type = NDIS_OBJECT_TYPE_DEFAULT;
            params.Header.Revision = NET_BUFFER_LIST_POOL_PARAMETERS_REVISION_1;
            params.Header.Size = core::mem::size_of::<NET_BUFFER_LIST_POOL_PARAMETERS>() as u16;
            params.fAllocateNetBuffer = 1;
            params.PoolTag = POOL_TAG;
            params.DataSize = 0;

            let pool_handle = NdisAllocateNetBufferListPool(core::ptr::null_mut(), &params);
            if pool_handle.is_null() {
                return Err("failed to allocate NET_BUFFER_LIST pool".to_string());
            }
            Ok(Self { pool_handle })
        }
    }

    /// Builds an NBL whose MDL describes caller-owned packet storage.
    ///
    /// # Safety
    ///
    /// `packet_data` must reside in nonpaged memory and its allocation must remain
    /// at the same address and stay live until the returned NBL and its MDL are
    /// freed with [`Self::free_net_buffer`]. No Rust access to the storage may race
    /// native access through the MDL.
    pub unsafe fn wrap_packet_in_nbl(
        &self,
        packet_data: &mut [u8],
    ) -> Result<*mut NET_BUFFER_LIST, String> {
        if self.pool_handle.is_null() {
            return Err("allocator not initialized".to_string());
        }
        unsafe {
            // Create MDL struct that will hold the buffer.
            let mdl = IoAllocateMdl(
                packet_data.as_mut_ptr() as _,
                packet_data.len() as u32,
                0,
                0,
                core::ptr::null_mut(),
            );
            if mdl.is_null() {
                return Err("failed to allocate mdl".to_string());
            }

            // Build mdl with packet_data buffer.
            MmBuildMdlForNonPagedPool(mdl);

            // Initialize NBL structure.
            let mut nbl = core::ptr::null_mut();
            let status = FwpsAllocateNetBufferAndNetBufferList0(
                self.pool_handle,
                0,
                0,
                mdl,
                0,
                packet_data.len(),
                &mut nbl,
            );
            if let Err(err) = check_ntstatus(status) {
                IoFreeMdl(mdl);
                return Err(err);
            }
            if nbl.is_null() {
                IoFreeMdl(mdl);
                return Err("WFP returned a null NET_BUFFER_LIST".to_string());
            }
            return Ok(nbl);
        }
    }

    /// Frees an NBL/MDL pair created by [`Self::wrap_packet_in_nbl`].
    ///
    /// # Safety
    ///
    /// `nbl` may be null. Otherwise ownership of the complete chain must be
    /// transferred to this function, every NBL must have been returned by
    /// `wrap_packet_in_nbl`, and no native or Rust code may access the NBLs or
    /// their MDLs after this call. Each packet's backing storage must remain live
    /// until this function returns; this function frees the native objects but
    /// not that storage.
    pub unsafe fn free_net_buffer(nbl: *mut NET_BUFFER_LIST) {
        // SAFETY: The caller guarantees that the complete owned chain remains
        // valid until it is consumed by the loop.
        let nbls = unsafe { NetBufferListIter::new(nbl) };
        nbls.for_each(|nbl| unsafe {
            if let Some(nbl) = nbl.nbl.as_mut() {
                // FwpsFreeNetBufferList0 destroys the NBL/NB objects, which
                // still contain the caller-owned MDL pointer. Release the
                // enclosing WFP objects before freeing that MDL.
                let mdl = nbl
                    .Header
                    .first_net_buffer
                    .as_ref()
                    .map(|nb| nb.MdlChain)
                    .unwrap_or(core::ptr::null_mut());
                FwpsFreeNetBufferList0(nbl);
                if !mdl.is_null() {
                    IoFreeMdl(mdl);
                }
            }
        });
    }
}

impl Drop for NetworkAllocator {
    fn drop(&mut self) {
        unsafe {
            if !self.pool_handle.is_null() {
                NdisFreeNetBufferListPool(self.pool_handle);
            }
        }
    }
}
