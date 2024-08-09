# WDK (Windows Driver Kit)

A library that interfaces with the windows kernel.  
The crate has extensive use of **unsafe** rust, be more causes when making changes.

Do not update `windows-sys` dependency.
The version contains bugs that have specific workarounds in this crate. Updating without reviewing the new version can result in broken build or undefined behavior.

see: `wdk/src/driver.rs`
see: `wdk/src/irp_helper.rs`

Open issues need to be resolved:
https://github.com/microsoft/windows-rs/issues/2805

Resolved:
https://github.com/microsoft/wdkmetadata/issues/59
