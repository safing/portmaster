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
