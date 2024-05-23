del {{version_file}}.cab

link.exe /OUT:{{sys_file}} `
/MANIFEST:NO /PROFILE /Driver `
"C:\Program Files (x86)\Windows Kits\10\lib\10.0.22621.0\km\x64\wdmsec.lib" `
"C:\Program Files (x86)\Windows Kits\10\lib\10.0.22621.0\km\x64\ndis.lib" `
"C:\Program Files (x86)\Windows Kits\10\lib\10.0.22621.0\km\x64\fwpkclnt.lib" `
"C:\Program Files (x86)\Windows Kits\10\lib\10.0.22621.0\um\x64\uuid.lib" `
"C:\Program Files (x86)\Windows Kits\10\lib\10.0.22621.0\km\x64\BufferOverflowK.lib" `
"C:\Program Files (x86)\Windows Kits\10\lib\10.0.22621.0\km\x64\ntoskrnl.lib" `
"C:\Program Files (x86)\Windows Kits\10\lib\10.0.22621.0\km\x64\hal.lib" `
"C:\Program Files (x86)\Windows Kits\10\lib\10.0.22621.0\km\x64\wmilib.lib" `
"C:\Program Files (x86)\Windows Kits\10\lib\wdf\kmdf\x64\1.15\WdfLdr.lib" `
"C:\Program Files (x86)\Windows Kits\10\lib\wdf\kmdf\x64\1.15\WdfDriverEntry.lib" `
"{{lib_file}}" `
/RELEASE /VERSION:"10.0" /DEBUG /MACHINE:X64 /ENTRY:"FxDriverEntry" /OPT:REF /INCREMENTAL:NO /SUBSYSTEM:NATIVE",6.01" /OPT:ICF /ERRORREPORT:PROMPT /MERGE:"_TEXT=.text;_PAGE=PAGE" /NOLOGO /NODEFAULTLIB /SECTION:"INIT,d" 

if(!$?) { 
    Exit $LASTEXITCODE 
}

move {{sys_file}} cab\\{{sys_file}}
move {{pdb_file}} cab\\{{pdb_file}}

Write-Host
Write-Host =====
Write-Host creating .cab ...
MakeCab /f {{version_file}}.ddf

if(!$?) { 
    Exit $LASTEXITCODE 
}

Write-Host
Write-Host =====
Write-Host cleaning up ...
del setup.inf
del setup.rpt
move disk1\\{{version_file}}.cab {{version_file}}.cab
rmdir disk1

Write-Host
Write-Host =====
Write-Host YOUR TURN: sign the .cab
Write-Host use something along the lines of:
Write-Host
Write-Host signtool sign /sha1 C2CBB3A0256A157FEB08B661D72BF490B68724C4 /tr http://timestamp.digicert.com /td sha256 /fd sha256 /a {{version_file}}.cab
Write-Host
