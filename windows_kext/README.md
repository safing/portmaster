# Portmaster Windows kext
Implementation of Safing's Portmaster Windows kernel extension in Rust.

### Documentation

- [Driver](driver/README.md) -> entry point.
- [WDK](wdk/README.md) -> Windows Driver Kit interface.
- [Packet Path](PacketFlow.md) -> Detailed documentation of what happens to a packet when it enters the kernel extension.
- [Release](release/README.md) -> Guide how to do a release build.
- [Windows Filtering Platform - MS](https://learn.microsoft.com/en-us/windows-hardware/drivers/network/roadmap-for-developing-wfp-callout-drivers) -> The driver is build on top of WFP.

### Building (For release)

Please refer to [release/README.md](release/README.md) for details about the release procedure.

### Building (For testing and development)

The Windows Portmaster Kernel Extension is currently only developed and tested for the amd64 (64-bit) architecture.

__Prerequirements:__

- Visual Studio 2022
    - Install C++ and Windows 11 SDK (22H2) components
    - Add `link.exe` and `signtool` in the PATH
- Windows Driver Kit
    - https://learn.microsoft.com/en-us/windows-hardware/drivers/download-the-wdk
- Rust (Can be separate machine)
    - https://www.rust-lang.org/tools/install

__Setup Test Signing:__

> Not recommended for a work machine. Usually done on virtual machine dedicated for testing.

In order to test the driver on your machine, you will have to sign it (starting with Windows 10).

Create a new certificate for test signing:

```ps1
    # Open a *x64 Free Build Environment* console as Administrator.

    # Run the MakeCert.exe tool to create a test certificate:
    MakeCert -r -pe -ss PrivateCertStore -n "CN=DriverCertificate" DriverCertificate.cer

    # Install the test certificate with CertMgr.exe:
    CertMgr /add DriverCertificate.cer /s /r localMachine root
```

Enable Test Signing on the dev machine:
```ps1
    # Before you can load test-signed drivers, you must enable Windows test mode. To do this, run this command:
    Bcdedit.exe -set TESTSIGNING ON
    # Then, restart Windows. For more information, see The TESTSIGNING Boot Configuration Option.
```

__Build driver:__

```sh
    cd driver
    cargo build --release
```
> Build also works on linux

__Link and sign:__
On a windows machine copy `driver.lib` from the project target directory (`driver/target/x86_64-pc-windows-msvc/release/driver.lib`) in the same folder as `link-dev.ps1`.
Run `link-dev.ps1`.

`driver.sys` should appear in the folder.

Sign the driver with the test certificate:
```
  SignTool sign /v /s TestCertStoreName /n TestCertName driver.sys
```
Load and use the driver.
