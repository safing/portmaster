# Example script for creating debug builds. Libraries may change depending on the version of the WDK that is installed.

$SDK_Version = "10.0.26100.0"

link.exe /OUT:driver.sys `
/MANIFEST:NO /PROFILE /Driver `
"C:\Program Files (x86)\Windows Kits\10\lib\$SDK_Version\km\x64\wdmsec.lib" `
"C:\Program Files (x86)\Windows Kits\10\lib\$SDK_Version\km\x64\ndis.lib" `
"C:\Program Files (x86)\Windows Kits\10\lib\$SDK_Version\km\x64\fwpkclnt.lib" `
"C:\Program Files (x86)\Windows Kits\10\lib\$SDK_Version\km\x64\BufferOverflowK.lib" `
"C:\Program Files (x86)\Windows Kits\10\lib\$SDK_Version\km\x64\ntoskrnl.lib" `
"C:\Program Files (x86)\Windows Kits\10\lib\$SDK_Version\km\x64\hal.lib" `
"C:\Program Files (x86)\Windows Kits\10\lib\$SDK_Version\km\x64\wmilib.lib" `
"C:\Program Files (x86)\Windows Kits\10\lib\wdf\kmdf\x64\1.15\WdfLdr.lib" `
"C:\Program Files (x86)\Windows Kits\10\lib\wdf\kmdf\x64\1.15\WdfDriverEntry.lib" `
 "driver.lib" `
/RELEASE /VERSION:"10.0" /DEBUG /MACHINE:X64 /ENTRY:"FxDriverEntry" /OPT:REF /INCREMENTAL:NO /SUBSYSTEM:NATIVE",6.01" /OPT:ICF /ERRORREPORT:PROMPT /MERGE:"_TEXT=.text;_PAGE=PAGE" /NOLOGO /NODEFAULTLIB /SECTION:"INIT,d"

if(!$?) {
    Exit $LASTEXITCODE
}
