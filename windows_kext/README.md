# Portmaster Windows kext
Implementation of Safing's Portmaster Windows kernel extension in Rust.

### Documentation 

- [Driver](driver/README.md) -> entry point.
- [WDK](wdk/README.md) -> Windows Driver Kit interface.
- [Packet Path](PacketDoc.md) -> Detiled documentation of what happens to a packet when it enters the kernel extension.
- [Release](release/README.md) -> Guide how to do a release build

### Building

The Windows Portmaster Kernel Extension is currently only developed and tested for the amd64 (64-bit) architecture.

__Prerequesites:__

- Visual Studio 2022
    - Install C++ and Windows 11 SDK (22H2) components
    - Add `link.exe` and `signtool` in the PATH
- Rust
    - https://www.rust-lang.org/tools/install
- Cargo make(optional)
    - https://github.com/sagiegurari/cargo-make

__Setup Test Signing:__

In order to test the driver on your machine, you will have to test sign it (starting with Windows 10).


Create a new certificate for test signing:

    :: Open a *x64 Free Build Environment* console as Administrator.

    :: Run the MakeCert.exe tool to create a test certificate:
    MakeCert -r -pe -ss PrivateCertStore -n "CN=DriverCertificate" DriverCertificate.cer

    :: Install the test certificate with CertMgr.exe:
    CertMgr /add DriverCertificate.cer /s /r localMachine root


Enable Test Signing on the dev machine:

    :: Before you can load test-signed drivers, you must enable Windows test mode. To do this, run this command:
    Bcdedit.exe -set TESTSIGNING ON
    :: Then, restart Windows. For more information, see The TESTSIGNING Boot Configuration Option.


__Build driver:__

```
cd driver
cargo build
```
> Build also works on linux

__Link and sign:__
On a windows machine copy `driver.lib` form the project target directory (`driver/target/x86_64-pc-windows-msvc/debug/driver.lib`) in the same folder as `link.bat`.
Run `link.bat`.

`driver.sys` should appear in the folder. Load and use the driver.

### Test
- Install go
    - https://go.dev/dl/

```
cd kext_tester
go run .
```

> make sure the hardcoded path in main.go is pointing to the correct `.sys` file
