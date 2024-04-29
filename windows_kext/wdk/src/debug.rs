#[cfg(debug_assertions)]
#[macro_export]
macro_rules! log {
    ($level:expr, $($arg:tt)*) => ({
        let message = alloc::format!($($arg)*);
        $crate::interface::dbg_print(alloc::format!("{} {}: {}", $level, core::module_path!(), message));
    });
}

#[cfg(not(debug_assertions))]
#[macro_export]
macro_rules! log {
    ($($arg:expr),*) => {{
        $(
            _ = $arg;
        )*
    }};
}

#[macro_export]
macro_rules! err {
    ($($arg:tt)*) => ($crate::log!("ERROR", $($arg)*));
}

#[macro_export]
macro_rules! dbg {
    ($($arg:tt)*) => ($crate::log!("DEBUG", $($arg)*));
}

#[macro_export]
macro_rules! info {
    ($($arg:tt)*) => ($crate::log!("INFO", $($arg)*));
}
