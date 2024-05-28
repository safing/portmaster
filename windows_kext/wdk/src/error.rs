// use anyhow::anyhow;

// pub fn anyhow_ntstatus(status: i32) -> anyhow::Error {
//     if let Some(value) = ntstatus::ntstatus::NtStatus::from_u32(status as u32) {
//         return anyhow!(value);
//     }

//     return anyhow!("UNKNOWN_NTSTATUS_CODE");
// }
