//! Windows ETW integration via `portmaster-core.dll`.
//!
//! Port of Go `service/integration`. The seven `PM_ETW*` entry points are
//! resolved from the DLL and wrapped with typed method calls. On non-Windows
//! targets the module is a no-op that reports [`IntegrationError::Unsupported`].

/// Errors of the OS integration module.
#[derive(Debug, thiserror::Error)]
pub enum IntegrationError {
    /// The operation only exists on Windows.
    #[error("operation is only supported on Windows")]
    Unsupported,
    /// The DLL could not be loaded.
    #[error("failed to load DLL: {0}")]
    LoadFailed(String),
    /// A required export was missing.
    #[error("failed to load function {0}: {1}")]
    MissingSymbol(&'static str, String),
    /// The DLL path contained an interior NUL byte.
    #[error("invalid DLL path: {0}")]
    InvalidPath(String),
    /// An ETW entry point returned a non-zero status.
    #[error("ETW call failed with status code {0}")]
    CallFailed(u32),
}

/// Result type of the OS integration module.
pub type Result<T> = std::result::Result<T, IntegrationError>;

#[cfg(windows)]
const CREATE_STATE: &str = "PM_ETWCreateState";
#[cfg(windows)]
const INITIALIZE_SESSION: &str = "PM_ETWInitializeSession";
#[cfg(windows)]
const START_TRACE: &str = "PM_ETWStartTrace";
#[cfg(windows)]
const FLUSH_TRACE: &str = "PM_ETWFlushTrace";
#[cfg(windows)]
const STOP_TRACE: &str = "PM_ETWStopTrace";
#[cfg(windows)]
const DESTROY_SESSION: &str = "PM_ETWDestroySession";
#[cfg(windows)]
const STOP_OLD_SESSION: &str = "PM_ETWStopOldSession";

#[cfg(windows)]
use ffi::Dll;

#[cfg(windows)]
mod ffi {
    use std::ffi::{c_char, c_void};

    /// Owns a loaded module and frees it on drop.
    pub(super) struct Dll(*mut c_void);

    // A Windows HMODULE is a module base address and may be shared between
    // threads; LoadLibrary/FreeLibrary are thread-safe.
    #[allow(unsafe_code)]
    unsafe impl Send for Dll {}
    #[allow(unsafe_code)]
    unsafe impl Sync for Dll {}

    // `unsafe` is confined to this extern block and the Dll destructor.
    #[allow(unsafe_code)]
    #[link(name = "kernel32")]
    unsafe extern "system" {
        pub(super) fn LoadLibraryA(name: *const c_char) -> *mut c_void;
        pub(super) fn GetProcAddress(module: *mut c_void, name: *const c_char) -> *mut c_void;
        pub(super) fn FreeLibrary(module: *mut c_void) -> i32;
    }

    #[allow(unsafe_code)]
    impl Dll {
        pub(super) fn load(path: &str) -> super::Result<Self> {
            use std::ffi::CString;

            let path = CString::new(path)
                .map_err(|err| super::IntegrationError::InvalidPath(err.to_string()))?;
            let handle = unsafe { LoadLibraryA(path.as_ptr()) };
            if handle.is_null() {
                return Err(super::IntegrationError::LoadFailed(path.to_string_lossy().into_owned()));
            }
            Ok(Dll(handle))
        }

        /// Resolves a `FARPROC` and casts it to the requested function type.
        pub(super) unsafe fn symbol<T>(&self, name: &'static str) -> super::Result<T> {
            let symbol = std::ffi::CString::new(name)
                .map_err(|err| super::IntegrationError::MissingSymbol(name, err.to_string()))?;
            let address = unsafe { GetProcAddress(self.0, symbol.as_ptr()) };
            if address.is_null() {
                return Err(super::IntegrationError::MissingSymbol(
                    name,
                    "symbol not found".to_string(),
                ));
            }
            Ok(std::mem::transmute_copy::<*mut c_void, T>(&address))
        }
    }

    #[allow(unsafe_code)]
    impl Drop for Dll {
        fn drop(&mut self) {
            unsafe {
                FreeLibrary(self.0);
            }
        }
    }
}

/// Function pointer types of the ETW entry points.
#[cfg(windows)]
mod signatures {
    /// `PM_ETWCreateState`: takes a callback pointer, returns an opaque state.
    pub(super) type CreateState = unsafe extern "system" fn(usize) -> usize;
    /// All other entry points take the opaque state and return a status code.
    pub(super) type StateCall = unsafe extern "system" fn(usize) -> u32;
    /// `PM_ETWStopOldSession` takes no arguments.
    pub(super) type StopOldSession = unsafe extern "system" fn() -> u32;
}

/// The loaded ETW function table.
///
/// On Windows it owns the DLL handle, so the exports stay valid as long as the
/// table lives. On other platforms it is a zero-sized no-op.
pub struct EtwFunctions {
    #[cfg(windows)]
    _dll: Dll,
    #[cfg(windows)]
    create_state: signatures::CreateState,
    #[cfg(windows)]
    initialize_session: signatures::StateCall,
    #[cfg(windows)]
    start_trace: signatures::StateCall,
    #[cfg(windows)]
    flush_trace: signatures::StateCall,
    #[cfg(windows)]
    stop_trace: signatures::StateCall,
    #[cfg(windows)]
    destroy_session: signatures::StateCall,
    #[cfg(windows)]
    stop_old_session: signatures::StopOldSession,
}

#[cfg(windows)]
#[allow(unsafe_code)]
impl EtwFunctions {
    /// Loads `path` and resolves every required ETW export.
    pub fn load(path: &str) -> Result<Self> {
        let dll = Dll::load(path)?;
        // Each symbol is fetched separately so errors name the missing export.
        let create_state = symbol(&dll, CREATE_STATE)?;
        let initialize_session = symbol(&dll, INITIALIZE_SESSION)?;
        let start_trace = symbol(&dll, START_TRACE)?;
        let flush_trace = symbol(&dll, FLUSH_TRACE)?;
        let stop_trace = symbol(&dll, STOP_TRACE)?;
        let destroy_session = symbol(&dll, DESTROY_SESSION)?;
        let stop_old_session = symbol(&dll, STOP_OLD_SESSION)?;
        Ok(Self {
            _dll: dll,
            create_state,
            initialize_session,
            start_trace,
            flush_trace,
            stop_trace,
            destroy_session,
            stop_old_session,
        })
    }

    /// Calls `PM_ETWCreateState` with the given callback pointer.
    pub fn create_state(&self, callback: usize) -> usize {
        unsafe { (self.create_state)(callback) }
    }

    /// Calls `PM_ETWInitializeSession`.
    pub fn initialize_session(&self, state: usize) -> Result<()> {
        status(unsafe { (self.initialize_session)(state) })
    }

    /// Calls `PM_ETWStartTrace`.
    pub fn start_trace(&self, state: usize) -> Result<()> {
        status(unsafe { (self.start_trace)(state) })
    }

    /// Calls `PM_ETWFlushTrace`.
    pub fn flush_trace(&self, state: usize) -> Result<()> {
        status(unsafe { (self.flush_trace)(state) })
    }

    /// Calls `PM_ETWStopTrace`.
    pub fn stop_trace(&self, state: usize) -> Result<()> {
        status(unsafe { (self.stop_trace)(state) })
    }

    /// Calls `PM_ETWDestroySession`.
    pub fn destroy_session(&self, state: usize) -> Result<()> {
        status(unsafe { (self.destroy_session)(state) })
    }

    /// Calls `PM_ETWStopOldSession`.
    pub fn stop_old_session(&self) -> Result<()> {
        status(unsafe { (self.stop_old_session)() })
    }
}

#[cfg(windows)]
#[allow(unsafe_code)]
fn symbol<T>(dll: &Dll, name: &'static str) -> Result<T> {
    unsafe { dll.symbol::<T>(name) }
        .map_err(|err| IntegrationError::MissingSymbol(name, err.to_string()))
}

#[cfg(windows)]
fn status(code: u32) -> Result<()> {
    if code == 0 {
        Ok(())
    } else {
        Err(IntegrationError::CallFailed(code))
    }
}

#[cfg(not(windows))]
impl EtwFunctions {
    /// Non-Windows stub: the DLL does not exist.
    pub fn load(_path: &str) -> Result<Self> {
        Err(IntegrationError::Unsupported)
    }

    /// Non-Windows no-op.
    pub fn create_state(&self, _callback: usize) -> usize {
        0
    }

    /// Non-Windows no-op.
    pub fn initialize_session(&self, _state: usize) -> Result<()> {
        Err(IntegrationError::Unsupported)
    }

    /// Non-Windows no-op.
    pub fn start_trace(&self, _state: usize) -> Result<()> {
        Err(IntegrationError::Unsupported)
    }

    /// Non-Windows no-op.
    pub fn flush_trace(&self, _state: usize) -> Result<()> {
        Err(IntegrationError::Unsupported)
    }

    /// Non-Windows no-op.
    pub fn stop_trace(&self, _state: usize) -> Result<()> {
        Err(IntegrationError::Unsupported)
    }

    /// Non-Windows no-op.
    pub fn destroy_session(&self, _state: usize) -> Result<()> {
        Err(IntegrationError::Unsupported)
    }

    /// Non-Windows no-op.
    pub fn stop_old_session(&self) -> Result<()> {
        Err(IntegrationError::Unsupported)
    }
}

/// Lifetime wrapper around the ETW integration, like Go's `OSIntegration`.
#[derive(Default)]
pub struct OsIntegration {
    etw: Option<EtwFunctions>,
}

impl OsIntegration {
    pub fn new() -> Self {
        Self::default()
    }

    /// Loads the DLL and its ETW exports, like Go `Initialize`/`loadDLL`.
    pub fn initialize(&mut self, dll_path: &str) -> Result<()> {
        self.etw = Some(EtwFunctions::load(dll_path)?);
        Ok(())
    }

    /// Releases the loaded DLL, like Go `CleanUp`.
    pub fn clean_up(&mut self) {
        self.etw = None;
    }

    /// Returns the loaded ETW table, or `None` before initialization.
    pub fn etw(&self) -> Option<&EtwFunctions> {
        self.etw.as_ref()
    }
}
