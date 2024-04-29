# Kext release tool

### Generate the zip file
- Make sure `kext_interface/version.txt` is up to date
- Execute: `cargo run`  
  * This will generate release `kext_release_vX-X-X.zip` file. Which contains all the necessary files to make the release.  

### Generate the cab file
- Copy the zip and extract it on a windows machine.
  * Some version Visual Studio needs to be installed.
- From VS Command Prompt / PowerShell run:
```
cd kext_release_v.../
./build_cab.bat
```

3. Sing the cab file

### Let Microsoft Sign
- Go to https://partner.microsoft.com/en-us/dashboard/hardware/driver/New
- Enter "PortmasterKext vX.X.X #1" as the product name
- Upload `portmaster-kext_vX-X-X.cab`
- Select the Windows 10 versions that you compiled and tested on
- Wait for the process to finish, download the `.zip`.

The zip will contain the release files.  
> Optionally sign the .sys file. 