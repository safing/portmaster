# Integration smoke test of portmaster-core on Windows. Runs inside the test
# VM as administrator (see test-windows.sh):
#
#   .\smoke.ps1 -Core C:\vmtest\portmaster-core.exe [-Dll C:\vmtest\portmaster-core.dll]
#
# With -Dll, the given core DLL replaces the downloaded one and must load.
# Fetches the signed release binaries (including the kernel extension) with
# the update command, then starts the given portmaster-core with packet
# interception and checks that it intercepts and filters traffic, answers its
# API and shuts down cleanly. Exits non-zero if any check fails.
param(
    [Parameter(Mandatory = $true)][string]$Core,
    [string]$Dll
)

$ErrorActionPreference = 'Continue'
$ProgressPreference = 'SilentlyContinue'

$Work = 'C:\vmtest\pm-smoke'
$Bin = Join-Path $Work 'bin'
$Data = Join-Path $Work 'data'
$Log = Join-Path $Work 'core.log'
$Api = 'http://127.0.0.1:817/api/v1'
$Curl = 'C:\Windows\System32\curl.exe'
$Allowed = 'example.com'
$Blocked = 'example.org'
# A name whose CNAME points outside the blocked zone (safing/portmaster#2306).
$CnameBlocked = 'tr.archive.ubuntu.com'
$CnameZone = 'ubuntu.com'

$script:failures = 0
function Pass($msg) { Write-Output "  PASS $msg" }
function Fail($msg) { Write-Output "  FAIL $msg"; $script:failures++ }
function Check($msg, [scriptblock]$test) {
    $ok = $false
    try { $ok = [bool](& $test) } catch { $ok = $false }
    if ($ok) { Pass $msg } else { Fail $msg }
}
function Step($msg) { Write-Output ''; Write-Output "== $msg" }

function HttpsOk($domain) {
    & $Curl -sS -o NUL --max-time 15 "https://$domain/" 2>$null
    return $LASTEXITCODE -eq 0
}
function HttpOk($domain) {
    & $Curl -sS -o NUL --max-time 15 "http://$domain/" 2>$null
    return $LASTEXITCODE -eq 0
}
function Resolves($domain) {
    $r = Resolve-DnsName -Name $domain -Type A -DnsOnly -QuickTimeout -ErrorAction SilentlyContinue |
        Where-Object { $_.IPAddress -and $_.IPAddress -ne '0.0.0.0' }
    return [bool]$r
}
function ApiReady {
    & $Curl -sf --max-time 5 "$Api/ready" 2>$null | Out-Null
    return $LASTEXITCODE -eq 0
}

Remove-Item -Recurse -Force $Work -ErrorAction SilentlyContinue
New-Item -ItemType Directory -Force -Path $Bin, $Data | Out-Null

Step 'Before start'
Check "https://$Allowed reachable" { HttpsOk $Allowed }
# This also puts the name into the DNS Client cache: Portmaster must flush it
# on start, or the connection would have no domain and pass the rule below.
Check "http://$CnameBlocked reachable" { HttpOk $CnameBlocked }

Step 'Release binaries'
& $Core update --bin-dir $Bin --data-dir $Data *>> $Log
$kext = Get-ChildItem -Path $Bin -Recurse -Filter 'portmaster-kext*.sys' -ErrorAction SilentlyContinue | Select-Object -First 1
if ($kext) {
    Pass "kernel extension downloaded ($($kext.Name), signed by $((Get-AuthenticodeSignature $kext.FullName).SignerCertificate.Subject))"
} else {
    Fail 'kernel extension downloaded'
    Get-Content $Log -Tail 30
    exit 1
}

if ($Dll) {
    $target = Get-ChildItem -Path $Bin -Recurse -Filter 'portmaster-core*.dll' -ErrorAction SilentlyContinue | Select-Object -First 1
    $targetPath = if ($target) { $target.FullName } else { Join-Path $Bin 'portmaster-core.dll' }
    Copy-Item -Force $Dll $targetPath
    Pass "core DLL replaced with $Dll"
}

# Block one domain in the global settings.
# Also allow incoming SSH, as the test is controlled over SSH.
Set-Content -Path (Join-Path $Data 'config.json') -Encoding ASCII `
    -Value ('{"filter":{"endpoints":["- ' + $Blocked + '","- .' + $CnameZone + '"],"serviceEndpoints":["+ * TCP/22"]}}')

Step 'Start'
$proc = Start-Process -FilePath $Core -PassThru -WindowStyle Hidden `
    -RedirectStandardOutput $Log -RedirectStandardError "$Log.err" `
    -ArgumentList @('--bin-dir', $Bin, '--data-dir', $Data, '--log-stdout', '--log', 'info', '--allowed-clients', $Curl)
# Without querying the handle, ExitCode stays empty after WaitForExit.
$null = $proc.Handle
$waited = 0
while (-not (ApiReady)) {
    Start-Sleep -Seconds 2
    $waited += 2
    if ($proc.HasExited -or $waited -ge 300) {
        Fail "portmaster-core starts and reports ready (exited: $($proc.HasExited), waited ${waited}s)"
        Get-Content $Log, "$Log.err" -Tail 30 -ErrorAction SilentlyContinue
        exit 1
    }
}
Pass "portmaster-core starts and reports ready (${waited}s)"
Check 'DNS Client cache flushed' { Select-String -Path $Log -Quiet -SimpleMatch 'flushed the DNS Client cache' }
Check 'kernel extension service running' { (Get-Service -Name 'PortmasterKext' -ErrorAction SilentlyContinue).Status -eq 'Running' -or (sc.exe query type= driver | Select-String -Quiet 'PortmasterKext') }

Step 'Filtering'
Start-Sleep -Seconds 3
Check "$Allowed resolves" { Resolves $Allowed }
Check "https://$Allowed reachable" { HttpsOk $Allowed }
# Windows resolves names through the DNS Client service (svchost), so the
# per-app rule applies to the connection, not to the DNS query.
Write-Output "  info: $Blocked resolves through the DNS Client service: $(Resolves $Blocked)"
Check "https://$Blocked is blocked" { -not (HttpsOk $Blocked) }
Check "http://$CnameBlocked is blocked by the rule for .$CnameZone (CNAME outside the zone)" { -not (HttpOk $CnameBlocked) }

Step 'API'
Check 'ping endpoint' { & $Curl -sf --max-time 5 "$Api/ping" 2>$null | Out-Null; $LASTEXITCODE -eq 0 }
Start-Sleep -Seconds 3
$body = '{"query":{"domain":{"$like":"%' + $Allowed + '%"}},"select":["id","verdict"],"pageSize":50}'
$bodyFile = Join-Path $Work 'query.json'
Set-Content -Path $bodyFile -Value $body -Encoding ASCII
$out = & $Curl -sf --max-time 10 -X POST "$Api/netquery/query" -H 'Content-Type: application/json' --data-binary "@$bodyFile" 2>$null
$n = 0
try { $n = @(($out | Out-String | ConvertFrom-Json).results).Count } catch {}
if ($n -gt 0) { Pass "netquery lists connections to $Allowed ($n)" } else { Fail "netquery lists connections to $Allowed (got '$out')" }
$body = '{"query":{"remote_ip":{"$in":["' + (@(Resolve-DnsName -Name $CnameBlocked -Type A -ErrorAction SilentlyContinue | Where-Object IPAddress | ForEach-Object IPAddress) -join '","') + '"]}},"select":["domain","verdict"],"groupBy":["domain","verdict"],"pageSize":20}'
Set-Content -Path $bodyFile -Value $body -Encoding ASCII
Write-Output "  info: connections to the IPs of ${CnameBlocked}: $(& $Curl -sf --max-time 10 -X POST "$Api/netquery/query" -H 'Content-Type: application/json' --data-binary "@$bodyFile" 2>$null)"
$code = 0
try {
    Invoke-WebRequest -UseBasicParsing -Method Post -Uri "$Api/netquery/query" -Body '{}' -TimeoutSec 10 | Out-Null
    $code = 200
} catch { $code = [int]$_.Exception.Response.StatusCode }
if ($code -eq 403 -or $code -eq 401) { Pass "API denies an unauthorized local client ($code)" } else { Fail "API denies an unauthorized local client (got $code)" }
# app/auth identifies the calling process and waits for the user to answer
# the prompt; without the process (safing/portmaster#2302 B1) it fails at once.
$authCode = & $Curl -s -o NUL -w '%{http_code}' --max-time 5 "$Api/app/auth?app-name=vmtest&read=user&write=user&ttl=1h" 2>$null
if ($authCode -eq '000') { Pass 'app/auth identifies the caller and waits for the prompt' } else { Fail "app/auth identifies the caller and waits for the prompt (got HTTP $authCode)" }

Step 'Load'
$jobs = 1..40 | ForEach-Object { Start-Process -FilePath $Curl -PassThru -WindowStyle Hidden -ArgumentList @('-s', '-o', 'NUL', '--max-time', '10', "https://$Allowed/?n=$_") }
$jobs | ForEach-Object { $_.WaitForExit(20000) | Out-Null }
Check 'service alive after 40 parallel connections' { -not $proc.HasExited }
Check "https://$Allowed still reachable" { HttpsOk $Allowed }

Step 'Shutdown'
& $Curl -sf --max-time 10 -X POST "$Api/core/shutdown" 2>$null | Out-Null
if ($proc.WaitForExit(60000)) { Pass 'portmaster-core stops within 60s' } else { Fail 'portmaster-core stops within 60s'; Stop-Process -Id $proc.Id -Force }
Check "exit code 0 (got $($proc.ExitCode))" { $proc.ExitCode -eq 0 }
Start-Sleep -Seconds 2
Check "https://$Allowed reachable without Portmaster" { HttpsOk $Allowed }
Check "$Blocked resolves again without Portmaster" { Resolves $Blocked }

Step 'Log'
Get-Content "$Log.err" -ErrorAction SilentlyContinue | Add-Content $Log
$panics = @(Select-String -Path $Log -Pattern '^panic:|fatal error:' -ErrorAction SilentlyContinue).Count
if ($panics -eq 0) { Pass 'no panics in the log' } else { Fail "$panics panics in the log" }
# The core DLL provides the ETW DNS integration. It fails to load when it
# needs a C runtime that the system does not have.
$dllLoaded = [bool](Select-String -Path $Log -Pattern 'integration: initialize successful' -SimpleMatch -ErrorAction SilentlyContinue)
if ($dllLoaded) {
    Pass 'core DLL loaded'
} elseif ($Dll) {
    Fail 'core DLL loaded'
} else {
    Write-Output '  info: core DLL not loaded (the upstream DLL needs the VC++ runtime)'
}
$errors = @(Select-String -Path $Log -Pattern ' (ERR|CRT) ' -CaseSensitive -ErrorAction SilentlyContinue).Count
Write-Output "  log: $(@(Get-Content $Log).Count) lines, $errors error lines (see core.log)"

Write-Output ''
if ($script:failures -eq 0) { Write-Output 'SMOKE TEST PASSED'; exit 0 }
Write-Output "SMOKE TEST FAILED: $($script:failures) checks"
exit 1
