# Building and Running Driver with Debug Logging

## Driver Signing Requirement

Windows requires **all kernel drivers to be signed**. Test signing provides a free alternative to expensive production code signing certificates for development and testing purposes.

## Important: Debug Builds Are Disabled

⚠️ **The driver cannot be compiled in debug mode.** The code contains a compile-time check (`compile_error!`) that prevents debug builds due to potential optimization-related issues and inconsistent compiler behavior between debug and release modes.

However, you can still enable verbose logging in release builds by changing the log level.

## Prerequisites

Already documented in [main README](../README.md), but quick recap:

1. **Visual Studio 2022** with C++ and Windows SDK
2. **Windows Driver Kit (WDK)** installed
3. **Rust toolchain** installed
4. **Test signing enabled** (see below)

## Step 1: Enable Test Signing (One-time Setup)

⚠️ **SECURITY WARNING**: Test signing reduces system security by allowing any locally-generated test certificate to load kernel drivers. **Strongly recommended to use a VM or dedicated test machine**. See "Disabling Test Signing" section below to restore security when done testing.

### Create Test Certificate

Open **PowerShell as Administrator**:

```powershell
# Create a self-signed certificate for driver testing
MakeCert -r -pe -ss PrivateCertStore -n "CN=DriverTestCert" DriverTestCert.cer

# Install the certificate to Trusted Root
CertMgr /add DriverTestCert.cer /s /r localMachine root

# Install to Trusted Publishers (needed for driver installation)
CertMgr /add DriverTestCert.cer /s /r localMachine trustedpublisher
```

### Enable Test Signing Mode

```powershell
# Enable test signing
Bcdedit.exe -set TESTSIGNING ON

# Restart required!
Restart-Computer
```

After restart, you should see **"Test Mode"** watermark in the corner of your screen.

### Verify Test Signing is Enabled

```powershell
bcdedit /enum | Select-String testsigning
# Should show: testsigning Yes
```

## Step 2: Enable Debug Logging in Driver

To see verbose logs from the driver, edit the log level before building.

**Edit `driver/src/logger.rs`:**

```rust
// Change line 8 from:
pub const LOG_LEVEL: u8 = Severity::Warning as u8;

// To one of:
pub const LOG_LEVEL: u8 = Severity::Debug as u8;   // Recommended for testing
// pub const LOG_LEVEL: u8 = Severity::Info as u8;    // Less verbose
// pub const LOG_LEVEL: u8 = Severity::Trace as u8;   // Most verbose
```

For testing, `Debug` level is recommended.

## Step 3: Build Driver in Release Mode

Navigate to the driver directory:

```powershell
cd D:\Projects\Portmaster\portmaster\windows_kext\driver

# Build in release mode (only mode supported)
cargo build --release --target x86_64-pc-windows-msvc

# Output: driver/target/x86_64-pc-windows-msvc/release/driver.lib
```

**Note:** Debug builds (`cargo build` without `--release`) will fail with a compile error by design.

## Step 4: Link the Driver

Copy the `.lib` file to the root directory:

```powershell
cd D:\Projects\Portmaster\portmaster\windows_kext

Copy-Item driver/target/x86_64-pc-windows-msvc/release/driver.lib . -Force
```

Run the linker script:

```powershell
.\link-dev.ps1
```

This creates `driver.sys` in the current directory.

## Step 5: Sign the Driver

## Step 5: Sign the Driver

```powershell
cd D:\Projects\Portmaster\portmaster\windows_kext

# Sign the driver
SignTool sign /v /s PrivateCertStore /n DriverTestCert driver.sys
```

Verify signature:

```powershell
SignTool verify /v /pa driver.sys
```

You should see: **"Successfully verified: driver.sys"**

## Step 6: View Driver Logs

### Ring Buffer Logs (Recommended)

These logs come through the `GetLogs` command.

### Kernel Debugger Output (Not Available in Release)

The `wdk::dbg!()`, `wdk::info!()`, and `wdk::err!()` macros only work in debug builds, which are disabled for this driver. These would output to tools like DebugView via `DbgPrint`, but since debug builds are not allowed, this logging path is not available.

**Use the ring buffer logs** (captured by `dbg!`, `info!`, `warn!`, `err!` macros) for all debugging.

## Common Issues

### "The hash for the file is not present in the specified catalog file"

**Solution**: Your driver isn't signed or the certificate isn't trusted.
```powershell
# Re-sign the driver
SignTool sign /v /s PrivateCertStore /n DriverTestCert driver.sys
```

### "Windows cannot verify the digital signature"

**Solution**: Test signing not enabled or certificate not in Trusted Root.
```powershell
# Check test signing
bcdedit /enum | Select-String testsigning

# Reinstall certificate if needed
CertMgr /add DriverTestCert.cer /s /r localMachine root
```

### "Service marked for deletion"

**Solution**: Manually clean up:
```powershell
sc stop PortmasterKext
sc delete PortmasterKext
# Wait a few seconds
# Then try starting again
```

### "Access is denied" when creating service

**Solution**: Run as Administrator.

### No debug output (`GetLogs` command)

**Solution**: 
1. Make sure you edited `driver/src/logger.rs` to set `LOG_LEVEL = Severity::Debug`
2. Rebuild the driver in **release mode** (`cargo build --release`)
3. The driver must be actively running and processing connections to generate logs
4. Default log level (`Warning`) only shows errors, not normal operations

## Quick Build & Test Cycle

```powershell
# 1. (Optional) Enable debug logging - edit driver/src/logger.rs first

# 2. Build driver in release mode
cd D:\Projects\Portmaster\portmaster\windows_kext\driver
cargo build --release

# 3. Link and sign
cd ..
Copy-Item driver/target/x86_64-pc-windows-msvc/release/driver.lib . -Force
.\link-dev.ps1
SignTool sign /v /s PrivateCertStore /n DriverTestCert driver.sys

# 4. Test (in playground, as Administrator)
```

## Disabling Test Signing (When Done Testing)

⚠️ **IMPORTANT**: When finished testing, disable test signing to restore system security.

```powershell
# Run as Administrator
Bcdedit.exe -set TESTSIGNING OFF

# Restart required for changes to take effect
Restart-Computer
```

After restart, the "Test Mode" watermark will disappear and the system will no longer accept test-signed drivers. This restores normal kernel driver security enforcement.Production vs Test Signing