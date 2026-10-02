// Commands from user space

use core::{convert::TryInto, mem::size_of};
use num_derive::FromPrimitive;
use num_traits::FromPrimitive;

#[repr(u8)]
#[derive(Clone, Copy, Debug, Eq, PartialEq, FromPrimitive)]
#[rustfmt::skip]
pub enum CommandType {
    Shutdown              = 0,
    Verdict               = 1,
    UpdateV4              = 2,
    UpdateV6              = 3,
    ClearCache            = 4,
    GetLogs               = 5,
    GetBandwidthStats     = 6,
    PrintMemoryStats      = 7,
    CleanEndedConnections = 8,
}

// These are private wire-layout markers. They must not be exposed as borrowed
// values: the protocol payload starts at an arbitrary byte address and is not
// guaranteed to satisfy the alignment of any field wider than one byte.
#[repr(C, packed)]
struct VerdictWire {
    id: u64,
    verdict: u8,
}

#[repr(C, packed)]
struct UpdateV4Wire {
    protocol: u8,
    local_address: [u8; 4],
    local_port: u16,
    remote_address: [u8; 4],
    remote_port: u16,
    verdict: u8,
}

#[repr(C, packed)]
struct UpdateV6Wire {
    protocol: u8,
    local_address: [u8; 16],
    local_port: u16,
    remote_address: [u8; 16],
    remote_port: u16,
    verdict: u8,
}

// Decoded command values use ordinary aligned Rust layout. The wire sizes are
// kept separately above because the Go and C++ clients serialize fields in
// packed logical order rather than using this in-memory representation.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct Verdict {
    pub id: u64,
    pub verdict: u8,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct UpdateV4 {
    pub protocol: u8,
    pub local_address: [u8; 4],
    pub local_port: u16,
    pub remote_address: [u8; 4],
    pub remote_port: u16,
    pub verdict: u8,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct UpdateV6 {
    pub protocol: u8,
    pub local_address: [u8; 16],
    pub local_port: u16,
    pub remote_address: [u8; 16],
    pub remote_port: u16,
    pub verdict: u8,
}

pub const VERDICT_PAYLOAD_SIZE: usize = size_of::<VerdictWire>();
pub const UPDATE_V4_PAYLOAD_SIZE: usize = size_of::<UpdateV4Wire>();
pub const UPDATE_V6_PAYLOAD_SIZE: usize = size_of::<UpdateV6Wire>();

const _: () = {
    assert!(VERDICT_PAYLOAD_SIZE == 9);
    assert!(UPDATE_V4_PAYLOAD_SIZE == 14);
    assert!(UPDATE_V6_PAYLOAD_SIZE == 38);
};

impl CommandType {
    /// Returns the exact payload size following the one-byte command type.
    pub const fn payload_size(self) -> usize {
        match self {
            Self::Shutdown
            | Self::ClearCache
            | Self::GetLogs
            | Self::GetBandwidthStats
            | Self::PrintMemoryStats
            | Self::CleanEndedConnections => 0,
            Self::Verdict => VERDICT_PAYLOAD_SIZE,
            Self::UpdateV4 => UPDATE_V4_PAYLOAD_SIZE,
            Self::UpdateV6 => UPDATE_V6_PAYLOAD_SIZE,
        }
    }
}

/// Returns the command type encoded in the first byte, if the byte is known.
pub fn parse_type(bytes: &[u8]) -> Option<CommandType> {
    bytes
        .first()
        .copied()
        .and_then(|value| CommandType::from_u8(value))
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum CommandParseError {
    Empty,
    UnknownType(u8),
    TruncatedPayload {
        command: CommandType,
        expected: usize,
        actual: usize,
    },
}

/// Splits the first complete command from a buffer of concatenated commands.
///
/// Command payload sizes are fixed by [`CommandType`], so no additional batch
/// header or per-command length field is required.
pub fn split_first_command(bytes: &[u8]) -> Result<(CommandType, &[u8], &[u8]), CommandParseError> {
    let Some(value) = bytes.first().copied() else {
        return Err(CommandParseError::Empty);
    };
    let Some(command) = CommandType::from_u8(value) else {
        return Err(CommandParseError::UnknownType(value));
    };

    let expected = command.payload_size();
    let payload_end = 1 + expected;
    if bytes.len() < payload_end {
        return Err(CommandParseError::TruncatedPayload {
            command,
            expected,
            actual: bytes.len() - 1,
        });
    }

    Ok((command, &bytes[1..payload_end], &bytes[payload_end..]))
}

/// Checks that a slice contains exactly one complete command payload.
pub fn has_valid_payload_length(command: CommandType, payload: &[u8]) -> bool {
    payload.len() == command.payload_size()
}

fn exact_payload(bytes: &[u8], expected: usize) -> Option<&[u8]> {
    (bytes.len() == expected).then_some(bytes)
}

fn read_u16_le(bytes: &[u8], offset: usize) -> Option<u16> {
    let value: [u8; 2] = bytes.get(offset..offset + 2)?.try_into().ok()?;
    Some(u16::from_le_bytes(value))
}

fn read_u64_le(bytes: &[u8], offset: usize) -> Option<u64> {
    let value: [u8; 8] = bytes.get(offset..offset + 8)?.try_into().ok()?;
    Some(u64::from_le_bytes(value))
}

fn read_array<const N: usize>(bytes: &[u8], offset: usize) -> Option<[u8; N]> {
    bytes.get(offset..offset + N)?.try_into().ok()
}

pub fn parse_verdict(bytes: &[u8]) -> Option<Verdict> {
    let bytes = exact_payload(bytes, VERDICT_PAYLOAD_SIZE)?;
    Some(Verdict {
        id: read_u64_le(bytes, 0)?,
        verdict: bytes[8],
    })
}

pub fn parse_update_v4(bytes: &[u8]) -> Option<UpdateV4> {
    let bytes = exact_payload(bytes, UPDATE_V4_PAYLOAD_SIZE)?;
    Some(UpdateV4 {
        protocol: bytes[0],
        local_address: read_array(bytes, 1)?,
        local_port: read_u16_le(bytes, 5)?,
        remote_address: read_array(bytes, 7)?,
        remote_port: read_u16_le(bytes, 11)?,
        verdict: bytes[13],
    })
}

pub fn parse_update_v6(bytes: &[u8]) -> Option<UpdateV6> {
    let bytes = exact_payload(bytes, UPDATE_V6_PAYLOAD_SIZE)?;
    Some(UpdateV6 {
        protocol: bytes[0],
        local_address: read_array(bytes, 1)?,
        local_port: read_u16_le(bytes, 17)?,
        remote_address: read_array(bytes, 19)?,
        remote_port: read_u16_le(bytes, 35)?,
        verdict: bytes[37],
    })
}

#[cfg(test)]
use std::fs::File;
#[cfg(test)]
use std::io::Read;
#[cfg(test)]
use std::panic;

#[test]
fn test_go_command_file() {
    let mut file = File::open("testdata/go_command_test.bin").unwrap();
    loop {
        let mut command: [u8; 1] = [0];
        let bytes_count = file.read(&mut command).unwrap();
        if bytes_count == 0 {
            return;
        }
        if let Some(command) = parse_type(&command) {
            match command {
                CommandType::Shutdown => {}
                CommandType::Verdict => {
                    let mut buf = [0; VERDICT_PAYLOAD_SIZE];
                    let bytes_count = file.read(&mut buf).unwrap();
                    if bytes_count != VERDICT_PAYLOAD_SIZE {
                        panic!("unexpected bytes count")
                    }

                    assert_eq!(parse_verdict(&buf), Some(Verdict { id: 1, verdict: 2 }))
                }
                CommandType::UpdateV4 => {
                    let mut buf = [0; UPDATE_V4_PAYLOAD_SIZE];
                    let bytes_count = file.read(&mut buf).unwrap();
                    if bytes_count != UPDATE_V4_PAYLOAD_SIZE {
                        panic!("unexpected bytes count")
                    }

                    assert_eq!(
                        parse_update_v4(&buf),
                        Some(UpdateV4 {
                            protocol: 1,
                            local_address: [1, 2, 3, 4],
                            local_port: 2,
                            remote_address: [2, 3, 4, 5],
                            remote_port: 3,
                            verdict: 4
                        })
                    )
                }
                CommandType::UpdateV6 => {
                    let mut buf = [0; UPDATE_V6_PAYLOAD_SIZE];
                    let bytes_count = file.read(&mut buf).unwrap();
                    if bytes_count != UPDATE_V6_PAYLOAD_SIZE {
                        panic!("unexpected bytes count")
                    }

                    assert_eq!(
                        parse_update_v6(&buf),
                        Some(UpdateV6 {
                            protocol: 1,
                            local_address: [
                                1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16
                            ],
                            local_port: 2,
                            remote_address: [
                                2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17
                            ],
                            remote_port: 3,
                            verdict: 4
                        })
                    )
                }
                CommandType::ClearCache => {}
                CommandType::GetLogs => {}
                CommandType::GetBandwidthStats => {}
                CommandType::PrintMemoryStats => {}
                CommandType::CleanEndedConnections => {}
            }
        } else {
            panic!("Unknown command: {}", command[0]);
        }
    }
}

#[test]
fn rejects_empty_and_wrong_sized_payloads() {
    assert_eq!(parse_type(&[]), None);
    assert!(parse_verdict(&[0; VERDICT_PAYLOAD_SIZE - 1]).is_none());
    assert!(parse_verdict(&[0; VERDICT_PAYLOAD_SIZE + 1]).is_none());
    assert!(parse_update_v4(&[0; UPDATE_V4_PAYLOAD_SIZE - 1]).is_none());
    assert!(parse_update_v6(&[0; UPDATE_V6_PAYLOAD_SIZE - 1]).is_none());
    assert!(!has_valid_payload_length(CommandType::Shutdown, &[0]));
    assert!(has_valid_payload_length(CommandType::Shutdown, &[]));
}

#[test]
fn splits_concatenated_commands() {
    let mut batch = vec![CommandType::GetLogs as u8, CommandType::Verdict as u8];
    batch.extend_from_slice(&[0x08, 0x07, 0x06, 0x05, 0x04, 0x03, 0x02, 0x01, 0x02]);
    batch.push(CommandType::ClearCache as u8);

    let (command, payload, remaining) = split_first_command(&batch).unwrap();
    assert_eq!(command, CommandType::GetLogs);
    assert!(payload.is_empty());

    let (command, payload, remaining) = split_first_command(remaining).unwrap();
    assert_eq!(command, CommandType::Verdict);
    assert_eq!(
        parse_verdict(payload),
        Some(Verdict {
            id: 0x0102_0304_0506_0708,
            verdict: 2,
        })
    );

    let (command, payload, remaining) = split_first_command(remaining).unwrap();
    assert_eq!(command, CommandType::ClearCache);
    assert!(payload.is_empty());
    assert!(remaining.is_empty());
}

#[test]
fn rejects_invalid_command_boundaries() {
    assert_eq!(split_first_command(&[]), Err(CommandParseError::Empty));
    assert_eq!(
        split_first_command(&[0xff]),
        Err(CommandParseError::UnknownType(0xff))
    );
    assert_eq!(
        split_first_command(&[CommandType::Verdict as u8, 1, 2, 3]),
        Err(CommandParseError::TruncatedPayload {
            command: CommandType::Verdict,
            expected: VERDICT_PAYLOAD_SIZE,
            actual: 3,
        })
    );
}

#[test]
fn decodes_unaligned_little_endian_payload() {
    let mut storage = [0_u8; VERDICT_PAYLOAD_SIZE + 1];
    storage[1..].copy_from_slice(&[
        0x08, 0x07, 0x06, 0x05, 0x04, 0x03, 0x02, 0x01, 0x0a,
    ]);

    assert_eq!(
        parse_verdict(&storage[1..]),
        Some(Verdict {
            id: 0x0102_0304_0506_0708,
            verdict: 10,
        })
    );
}
