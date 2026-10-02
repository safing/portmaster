//! Compile-time ABI checks for generated leaf types shared by the hand-written
//! kernel structures. Composite layouts are checked beside their declarations.

#[cfg(target_pointer_width = "64")]
const _: () = {
    use core::mem::{align_of, size_of};
    use windows_sys::{
        core::GUID,
        Wdk::Foundation::{IRP, MDL},
        Win32::{
            Foundation::{BOOLEAN, HANDLE, NTSTATUS, UNICODE_STRING},
            NetworkManagement::{IpHelper::IP_ADDRESS_PREFIX, WindowsFilteringPlatform::FWP_DIRECTION},
            Networking::WinSock::{ADDRESS_FAMILY, SCOPE_ID},
            System::{Kernel::{COMPARTMENT_ID, LIST_ENTRY}, IO::IO_STATUS_BLOCK},
        },
    };

    assert!(size_of::<BOOLEAN>() == 1);
    assert!(align_of::<BOOLEAN>() == 1);
    assert!(size_of::<u8>() == 1); // KIRQL and KPROCESSOR_MODE are CCHAR/UCHAR.
    assert!(align_of::<u8>() == 1);
    assert!(size_of::<HANDLE>() == 8);
    assert!(align_of::<HANDLE>() == 8);
    assert!(size_of::<usize>() == 8); // SIZE_T
    assert!(align_of::<usize>() == 8);
    assert!(size_of::<NTSTATUS>() == 4);
    assert!(align_of::<NTSTATUS>() == 4);
    assert!(size_of::<ADDRESS_FAMILY>() == 2);
    assert!(align_of::<ADDRESS_FAMILY>() == 2);
    assert!(size_of::<COMPARTMENT_ID>() == 4);
    assert!(align_of::<COMPARTMENT_ID>() == 4);
    assert!(size_of::<FWP_DIRECTION>() == 4);
    assert!(align_of::<FWP_DIRECTION>() == 4);

    assert!(size_of::<GUID>() == 16);
    assert!(align_of::<GUID>() == 4);
    assert!(size_of::<UNICODE_STRING>() == 16);
    assert!(align_of::<UNICODE_STRING>() == 8);
    assert!(size_of::<LIST_ENTRY>() == 16);
    assert!(align_of::<LIST_ENTRY>() == 8);
    assert!(size_of::<IO_STATUS_BLOCK>() == 16);
    assert!(align_of::<IO_STATUS_BLOCK>() == 8);
    assert!(size_of::<MDL>() == 48);
    assert!(align_of::<MDL>() == 8);
    assert!(size_of::<IRP>() == 208);
    assert!(align_of::<IRP>() == 8);
    assert!(size_of::<SCOPE_ID>() == 4);
    assert!(align_of::<SCOPE_ID>() == 4);
    assert!(size_of::<IP_ADDRESS_PREFIX>() == 32);
    assert!(align_of::<IP_ADDRESS_PREFIX>() == 4);
};

#[cfg(target_pointer_width = "64")]
const _: () = {
    use core::mem::{align_of, size_of};
    use windows_sys::Wdk::Foundation::KQUEUE;

    assert!(size_of::<KQUEUE>() == 64);
    assert!(align_of::<KQUEUE>() == 8);
};
