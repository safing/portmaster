//! Concrete process tag handlers.
//!
//! These are the Rust counterparts of the Linux handlers in Go's
//! `service/process/tags` package. They are not self-registering; use
//! [`register_default_tag_handlers`] to attach all of them to a
//! [`ProcessCache`], mirroring the Go `init()` functions.

pub mod net;
#[cfg(not(windows))]
pub mod appimage;
#[cfg(not(windows))]
pub mod flatpak;
#[cfg(not(windows))]
pub mod interpreter;
#[cfg(not(windows))]
pub mod snap;
#[cfg(windows)]
pub mod svchost;
#[cfg(windows)]
pub mod winstore;

use std::sync::Arc;

use crate::cache::Result;
use crate::ProcessCache;

/// Registers every built-in tag handler on the given cache.
///
/// The order matches the Go `init()` order (source file name order), which is
/// significant for `CreateProfile` in Go. The Rust `TagHandler` trait has no
/// `CreateProfile` callback, but the order is kept for deterministic tags.
pub fn register_default_tag_handlers(cache: &ProcessCache) -> Result<()> {
    // Windows only has the network, svchost and Windows Store handlers, like
    // Go's build-tag selection in `service/process/tags`.
    #[cfg(windows)]
    {
        cache.register_tag_handler(Arc::new(net::NetworkHandler))?;
        cache.register_tag_handler(Arc::new(svchost::SvchostHandler))?;
        cache.register_tag_handler(Arc::new(winstore::WinStoreHandler))?;
    }
    #[cfg(not(windows))]
    {
        cache.register_tag_handler(Arc::new(appimage::AppImageHandler::default()))?;
        cache.register_tag_handler(Arc::new(flatpak::FlatpakHandler))?;
        cache.register_tag_handler(Arc::new(interpreter::InterpHandler))?;
        cache.register_tag_handler(Arc::new(net::NetworkHandler))?;
        cache.register_tag_handler(Arc::new(snap::SnapHandler))?;
    }
    Ok(())
}
