# Runs once at the first logon of the Windows test VM (see autounattend.xml).
# Sets up the OpenSSH server with the suite's public key, so that the host
# can copy and run tests. Progress is logged to C:\vmtest-setup.log.
param([Parameter(Mandatory = $true)][string]$ConfigDrive)

$ErrorActionPreference = 'Stop'
Start-Transcript -Path 'C:\vmtest-setup-transcript.log' -Append | Out-Null

# Keep the VM awake.
powercfg /change standby-timeout-ac 0
powercfg /change monitor-timeout-ac 0
powercfg /hibernate off

# Install the OpenSSH server. It is a feature on demand and is fetched from
# Windows Update, which may not be reachable right after setup.
for ($i = 1; $i -le 30; $i++) {
    try {
        $cap = Get-WindowsCapability -Online -Name 'OpenSSH.Server*' | Select-Object -First 1
        if ($cap.State -ne 'Installed') {
            Add-WindowsCapability -Online -Name $cap.Name | Out-Null
        }
        break
    } catch {
        Write-Output "OpenSSH install attempt $i failed: $_"
        Start-Sleep -Seconds 30
    }
}

Set-Service -Name sshd -StartupType Automatic
Start-Service sshd

# Use PowerShell as the SSH shell.
New-ItemProperty -Path 'HKLM:\SOFTWARE\OpenSSH' -Name DefaultShell `
    -Value 'C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe' -PropertyType String -Force | Out-Null

# Authorize the suite's key for the administrator account.
$keys = 'C:\ProgramData\ssh\administrators_authorized_keys'
Copy-Item (Join-Path $ConfigDrive 'vmtest_id_ed25519.pub') $keys -Force
# Use SIDs, as group names are localized (Administrators, SYSTEM).
icacls $keys /inheritance:r /grant '*S-1-5-32-544:F' /grant '*S-1-5-18:F' | Out-Null

if (-not (Get-NetFirewallRule -Name 'vmtest-sshd' -ErrorAction SilentlyContinue)) {
    New-NetFirewallRule -Name 'vmtest-sshd' -DisplayName 'OpenSSH (vmtest)' -Direction Inbound `
        -Protocol TCP -LocalPort 22 -Action Allow | Out-Null
}

# Work directory for tests, excluded from Defender scans to keep runs fast.
New-Item -ItemType Directory -Force -Path 'C:\vmtest' | Out-Null
Add-MpPreference -ExclusionPath 'C:\vmtest' -ErrorAction SilentlyContinue

Restart-Service sshd
Set-Content -Path 'C:\vmtest-ready.txt' -Value (Get-Date -Format o)
Write-Output 'vmtest setup done'
