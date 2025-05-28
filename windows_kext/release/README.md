# Kext release tool

## Generate the zip file

- Make sure the deriver version in `kextinterface/version.txt` is up to date

- Execute: `cargo run`  
  _This will generate release `portmaster-kext-release-bundle-vX-X-X-X.zip` file. Which contains all the necessary files to make the release._

## Generate the cab file

  **Precondition:** Visual Studio 2022 and WDK need to be installed.

- copy the zip and extract it on a windows machine.

- update `.\build_cab.ps1`: set correct SDK version you use.
  _e.g.: $SDK_Version = "10.0.26100.0" (see in `C:\Program Files (x86)\Windows Kits\10\Lib`)_

- Use "Developer PowerShell for VS":

  ```powershell
  cd portmaster-kext-release-bundle-v...
  .\build_cab.ps1
  ```

- Sing the the output cab file: `portmaster-kext-release-bundle-v...\PortmasterKext_v....cab`

## Let Microsoft Sign

- Go to https://partner.microsoft.com/en-us/dashboard/hardware/driver/New
- Enter "PortmasterKext vX.X.X #1" as the product name
- Upload `portmaster-kext_vX-X-X.cab`
- Select the Windows 10 versions that you compiled and tested on
  - Currently: Windows 11 Client, version 22H2 x64 (Ni)
- Wait for the process to finish, download the `.zip`.

The zip will contain the release files.  
> Optionally sign the .sys file, with company certificate.
