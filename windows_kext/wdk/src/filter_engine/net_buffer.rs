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

impl NetBufferList {
    pub fn new(nbl: *mut NET_BUFFER_LIST) -> NetBufferList {
        NetBufferList {
            nbl,
            data: None,
            advance_on_drop: None,
        }
    }

    pub fn iter(&self) -> NetBufferListIter {
        NetBufferListIter(self.nbl)
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

                let ptr = NdisGetDataBuffer(nb, data_length, buffer.as_mut_ptr(), 1, 0);

                if !ptr.is_null() {
                    buffer.copy_from_slice(core::slice::from_raw_parts(ptr, data_length as usize));
                } else {
                    let ptr = NdisGetDataBuffer(nb, data_length, buffer.as_mut_ptr(), 1, 0);
                    if ptr.is_null() {
                        return Err("failed to copy packet buffer".to_string());
                    }
                }

                let new_nbl = net_allocator.wrap_packet_in_nbl(&buffer)?;

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

    /// Retreats the mnl of the buffer. Does not auto advance multiple retreats.
    pub fn retreat(&mut self, size: u32, auto_advance: bool) {
        unsafe {
            if let Some(nbl) = self.nbl.as_mut() {
                if let Some(nb) = nbl.Header.first_net_buffer.as_mut() {
                    NdisRetreatNetBufferDataStart(nb as _, size, 0, core::ptr::null_mut());
                    if auto_advance {
                        self.advance_on_drop = Some(size);
                    }
                }
            }
        }
    }

    /// Advances the MDL of the buffer.
    pub fn advance(&self, size: u32) {
        unsafe {
            if let Some(nbl) = self.nbl.as_mut() {
                if let Some(nb) = nbl.Header.first_net_buffer.as_mut() {
                    NdisAdvanceNetBufferDataStart(nb as _, size, false, core::ptr::null_mut());
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
            NetworkAllocator::free_net_buffer(self.nbl);
        }
    }
}

pub struct NetBufferListIter(*mut NET_BUFFER_LIST);

impl NetBufferListIter {
    pub fn new(nbl: *mut NET_BUFFER_LIST) -> Self {
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

pub fn read_packet_partial(nbl: *mut NET_BUFFER_LIST, buffer: &mut [u8]) -> Result<(), ()> {
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

            let ptr = NdisGetDataBuffer(nb, buffer.len() as u32, buffer.as_mut_ptr(), 1, 0);
            if !ptr.is_null() {
                return Ok(());
            }
        }
    }
    return Err(());
}

pub struct RetreatGuard {
    size: u32,
    nbl: *mut NET_BUFFER_LIST,
}

impl Drop for RetreatGuard {
    fn drop(&mut self) {
        NetworkAllocator::advance_net_buffer(self.nbl, self.size);
    }
}

pub struct NetworkAllocator {
    pool_handle: NDIS_HANDLE,
}

impl NetworkAllocator {
    pub fn new() -> Self {
        unsafe {
            let mut params: NET_BUFFER_LIST_POOL_PARAMETERS = MaybeUninit::zeroed().assume_init();
            params.Header.Type = NDIS_OBJECT_TYPE_DEFAULT;
            params.Header.Revision = NET_BUFFER_LIST_POOL_PARAMETERS_REVISION_1;
            params.Header.Size = core::mem::size_of::<NET_BUFFER_LIST_POOL_PARAMETERS>() as u16;
            params.fAllocateNetBuffer = true;
            params.PoolTag = POOL_TAG;
            params.DataSize = 0;

            let pool_handle = NdisAllocateNetBufferListPool(core::ptr::null_mut(), &params);
            Self { pool_handle }
        }
    }

    pub fn wrap_packet_in_nbl(&self, packet_data: &[u8]) -> Result<*mut NET_BUFFER_LIST, String> {
        if self.pool_handle.is_null() {
            return Err("allocator not initialized".to_string());
        }
        unsafe {
            // Create MDL struct that will hold the buffer.
            let mdl = IoAllocateMdl(
                packet_data.as_ptr() as _,
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
                packet_data.len() as u64,
                &mut nbl,
            );
            if let Err(err) = check_ntstatus(status) {
                IoFreeMdl(mdl);
                return Err(err);
            }
            return Ok(nbl);
        }
    }

    pub fn free_net_buffer(nbl: *mut NET_BUFFER_LIST) {
        NetBufferListIter::new(nbl).for_each(|nbl| unsafe {
            if let Some(nbl) = nbl.nbl.as_mut() {
                if let Some(nb) = nbl.Header.first_net_buffer.as_mut() {
                    IoFreeMdl(nb.MdlChain);
                }
                FwpsFreeNetBufferList0(nbl);
            }
        });
    }

    pub fn retreat_net_buffer(
        nbl: *mut NET_BUFFER_LIST,
        size: u32,
        auto_advance: bool,
    ) -> Option<RetreatGuard> {
        unsafe {
            if let Some(nbl) = nbl.as_mut() {
                if let Some(nb) = nbl.Header.first_net_buffer.as_mut() {
                    NdisRetreatNetBufferDataStart(nb as _, size, 0, core::ptr::null_mut());
                    if auto_advance {
                        return Some(RetreatGuard { size, nbl });
                    }
                }
            }
        }

        return None;
    }
    pub fn advance_net_buffer(nbl: *mut NET_BUFFER_LIST, size: u32) {
        unsafe {
            if let Some(nbl) = nbl.as_mut() {
                if let Some(nb) = nbl.Header.first_net_buffer.as_mut() {
                    NdisAdvanceNetBufferDataStart(nb as _, size, false, core::ptr::null_mut());
                }
            }
        }
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
