# Kext release tool

### Generate the zip file

- Make sure `kextinterface/version.txt` is up to date
- Execute: `cargo run`  
  * This will generate release `kext_release_vX-X-X.zip` file. Which contains all the necessary files to make the release.  

### Generate the cab file

- Copy the zip and extract it on a windows machine.
  * Visual Studio 2022 and WDK need to be installed.
- From VS Command Prompt / PowerShell run:
```
cd kext_release_v.../
./build_cab.bat
```
> Script is written for VS `$SDK_Version = "10.0.22621.0"`. If different version is used update the script.

- Sing the cab file

### Let Microsoft Sign

- Go to https://partner.microsoft.com/en-us/dashboard/hardware/driver/New
- Enter "PortmasterKext vX.X.X #1" as the product name
- Upload `portmaster-kext_vX-X-X.cab`
- Select the Windows 10 versions that you compiled and tested on
  - Currently: Windows 11 Client, version 22H2 x64 (Ni)
- Wait for the process to finish, download the `.zip`.

The zip will contain the release files.  
> Optionally sign the .sys file, with company certificate.
