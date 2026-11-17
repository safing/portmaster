# Runs smoke.ps1 as a scheduled task (see test-windows.sh) and records its
# output and exit code for the host to collect.
& C:\vmtest\smoke.ps1 -Core C:\vmtest\portmaster-core.exe *> C:\vmtest\smoke.txt
Set-Content -Path C:\vmtest\smoke.done -Value $LASTEXITCODE
