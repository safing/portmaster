use alloc::vec::Vec;
use protocol::info::{Info, Severity};
use wdk::rw_spin_lock::RwSpinLock;

pub const LOG_LEVEL: u8 = Severity::Warning as u8;
// pub const LOG_LEVEL: u8 = Severity::Trace as u8;

pub const MAX_LOG_LINE_SIZE: usize = 150;
const SIZE_OF_LOG_LINE_BUFFER: usize = 1024;

struct LogBuffer {
    lines: [Option<Info>; SIZE_OF_LOG_LINE_BUFFER],
    start: usize,
    len: usize,
}

impl LogBuffer {
    const fn new() -> Self {
        Self {
            lines: [const { None }; SIZE_OF_LOG_LINE_BUFFER],
            start: 0,
            len: 0,
        }
    }
}

static LOG_BUFFER: RwSpinLock<LogBuffer> = RwSpinLock::new(LogBuffer::new());

pub fn add_line(log_line: Info) {
    // Move ownership into the ring while holding the lock. Any overwritten line
    // is dropped only after the lock restores the caller's original IRQL.
    let old = {
        let mut buffer = LOG_BUFFER.write_lock();
        if buffer.len < SIZE_OF_LOG_LINE_BUFFER {
            let index = (buffer.start + buffer.len) % SIZE_OF_LOG_LINE_BUFFER;
            let old = buffer.lines[index].replace(log_line);
            buffer.len += 1;
            old
        } else {
            let index = buffer.start;
            let old = buffer.lines[index].replace(log_line);
            buffer.start = (buffer.start + 1) % SIZE_OF_LOG_LINE_BUFFER;
            old
        }
    };
    drop(old);
}

pub fn flush() -> Vec<Info> {
    // Reserve before taking the spin lock so moving the buffered lines below
    // cannot allocate while the lock is held at DISPATCH_LEVEL.
    let mut lines = Vec::with_capacity(SIZE_OF_LOG_LINE_BUFFER);
    {
        let mut buffer = LOG_BUFFER.write_lock();
        for offset in 0..buffer.len {
            let index = (buffer.start + offset) % SIZE_OF_LOG_LINE_BUFFER;
            if let Some(line) = buffer.lines[index].take() {
                lines.push(line);
            }
        }
        buffer.start = 0;
        buffer.len = 0;
    }
    lines
}

#[macro_export]
macro_rules! log_internal {
    ($log_line:expr, $($arg:tt)*) => ({
        use core::fmt::Write;
        _ = write!($log_line, "{}:{} ", file!(), line!());
        _ = write!($log_line, $($arg)*);
        $crate::logger::add_line($log_line);
    });
}

#[macro_export]
macro_rules! crit {
    ($($arg:tt)*) => ({
        if protocol::info::Severity::Critical as u8 >= $crate::logger::LOG_LEVEL {
            let mut log_line = protocol::info::log_line(protocol::info::Severity::Critical, $crate::logger::MAX_LOG_LINE_SIZE);
            $crate::log_internal!(log_line, $($arg)*);
        }
    });
}

#[macro_export]
macro_rules! err {
    ($($arg:tt)*) => ({
        if protocol::info::Severity::Error as u8 >= $crate::logger::LOG_LEVEL {
            let mut log_line = protocol::info::log_line(protocol::info::Severity::Error, $crate::logger::MAX_LOG_LINE_SIZE);
            $crate::log_internal!(log_line, $($arg)*);
        }
    });
}

#[macro_export]
macro_rules! warn {
    ($($arg:tt)*) => ({
        if protocol::info::Severity::Warning as u8 >= $crate::logger::LOG_LEVEL {
            let mut log_line = protocol::info::log_line(protocol::info::Severity::Warning, $crate::logger::MAX_LOG_LINE_SIZE);
            $crate::log_internal!(log_line, $($arg)*);
        }
    });
}

#[macro_export]
macro_rules! dbg {
    ($($arg:tt)*) => ({
        if protocol::info::Severity::Debug as u8 >= $crate::logger::LOG_LEVEL {
            let mut log_line = protocol::info::log_line(protocol::info::Severity::Debug, $crate::logger::MAX_LOG_LINE_SIZE);
            $crate::log_internal!(log_line, $($arg)*);
        }
    });
}

#[macro_export]
macro_rules! info {
    ($($arg:tt)*) => ({
        if protocol::info::Severity::Info as u8 >= $crate::logger::LOG_LEVEL {
            let mut log_line = protocol::info::log_line(protocol::info::Severity::Info, $crate::logger::MAX_LOG_LINE_SIZE);
            $crate::log_internal!(log_line, $($arg)*);
        }
    });
}
