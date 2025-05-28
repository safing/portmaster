# Remove previous cab build
Remove-Item -Path "PortmasterKext_v2-0-0.cab" -ErrorAction SilentlyContinue

$SDK_Version = "10.0.26100.0"

# Build metadata file
rc -I "C:\Program Files (x86)\Windows Kits\10\Include\$SDK_Version\um" `
   -I "C:\Program Files (x86)\Windows Kits\10\Include\$SDK_Version\shared" `
    .\version.rc

# Link the driver.
link.exe /OUT:{{sys_file}} `
/MANIFEST:NO /PROFILE /Driver `
"C:\Program Files (x86)\Windows Kits\10\lib\$SDK_Version\km\x64\wdmsec.lib" `
"C:\Program Files (x86)\Windows Kits\10\lib\$SDK_Version\km\x64\ndis.lib" `
"C:\Program Files (x86)\Windows Kits\10\lib\$SDK_Version\km\x64\fwpkclnt.lib" `
"C:\Program Files (x86)\Windows Kits\10\lib\$SDK_Version\um\x64\uuid.lib" `
"C:\Program Files (x86)\Windows Kits\10\lib\$SDK_Version\km\x64\BufferOverflowK.lib" `
"C:\Program Files (x86)\Windows Kits\10\lib\$SDK_Version\km\x64\ntoskrnl.lib" `
"C:\Program Files (x86)\Windows Kits\10\lib\$SDK_Version\km\x64\hal.lib" `
"C:\Program Files (x86)\Windows Kits\10\lib\$SDK_Version\km\x64\wmilib.lib" `
"C:\Program Files (x86)\Windows Kits\10\lib\wdf\kmdf\x64\1.15\WdfLdr.lib" `
"C:\Program Files (x86)\Windows Kits\10\lib\wdf\kmdf\x64\1.15\WdfDriverEntry.lib" `
"{{lib_file}}" "version.res" `
/RELEASE /VERSION:"10.0" /DEBUG /MACHINE:X64 /ENTRY:"FxDriverEntry" /OPT:REF /INCREMENTAL:NO /SUBSYSTEM:NATIVE",6.01" /OPT:ICF /ERRORREPORT:PROMPT /MERGE:"_TEXT=.text;_PAGE=PAGE" /NOLOGO /NODEFAULTLIB /SECTION:"INIT,d" 
if(!$?) { 
    Exit $LASTEXITCODE 
}

# Move the driver and debug symbolds into the cab directory.
move {{sys_file}} cab\\PortmasterKext64.sys
move {{pdb_file}} cab\\PortmasterKext64.pdb

# Create the cab.
Write-Host
Write-Host =====
Write-Host creating .cab ...
MakeCab /f PortmasterKext.ddf
if(!$?) { 
    Exit $LASTEXITCODE 
}

# Clean up after cab creation.
Write-Host
Write-Host =====
Write-Host cleaning up ...
Remove-Item -Path "setup.inf" -ErrorAction SilentlyContinue
Remove-Item -Path "setup.rpt" -ErrorAction SilentlyContinue
Move-Item disk1\\{{cab_file}} {{cab_file}}
Remove-Item disk1

# Print signing instructions.
Write-Host
Write-Host =====
Write-Host YOUR TURN: sign the .cab
Write-Host "(If the sha1 fingerprint of the cert has changed, you can find it in the cert properties on Windows as Thumbprint)"
Write-Host
Write-Host signtool sign /sha1 69ADFEACD5AC42D0DB5698E38CA917B9C60FBFA6 /tr http://timestamp.digicert.com /td sha256 /fd sha256 /a {{cab_file}}
Write-Host
