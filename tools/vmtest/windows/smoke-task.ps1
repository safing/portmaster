# Runs smoke.ps1 as a scheduled task (see test-windows.sh) and records its
# output and exit code for the host to collect.
$dll = @{}
if (Test-Path C:\vmtest\portmaster-core.dll) { $dll = @{ Dll = 'C:\vmtest\portmaster-core.dll' } }
& C:\vmtest\smoke.ps1 -Core C:\vmtest\portmaster-core.exe @dll *> C:\vmtest\smoke.txt
Set-Content -Path C:\vmtest\smoke.done -Value $LASTEXITCODE
