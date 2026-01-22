# Driver Test Playground

> ⚠️ **Notice**: This project was primarily generated with the assistance of AI and may contain errors or inaccuracies. It is intended solely for local testing and development and must not be used in production.

A simple Go console application for testing the Portmaster Windows kernel driver.

## Features

- **Driver Management**: Start/stop the kernel driver service
- **Connection Monitoring**: Logs all connection events from the driver
- **Auto-Verdict**: Sends `PermanentAccept` verdict for all connections by default
- **Redirect Mode**: Can redirect TCP/UDP traffic through a specified interface
- **Flexible Logging**: Output to console, file, or both
- **Log Filtering**: Filter terminal output with regex patterns (file logs remain unfiltered)
- **Driver Logs**: Periodically polls and displays driver internal logs

## Prerequisites

1. **Test-signed driver** (`PortmasterKext_test.sys`) - Build using `test\build_test.ps1`
2. **Test signing enabled** on the machine:
   ```powershell
   Bcdedit.exe -set TESTSIGNING ON
   # Restart required
   ```
3. **Administrator privileges** to install/start kernel drivers

## Building

### Build Driver
```powershell
# From windows_kext\test directory
.\build_test.ps1
```

### Build Playground
```powershell
cd test\playground
go build
```

## Usage

```powershell
# Run as Administrator!
.\playground.exe [options]
```

### Command Line Options

| Option | Default | Description |
|--------|---------|-------------|
| `-console` | `true` | Log to console |
| `-file` | `false` | Log to file |
| `-app-log` | `""` | Application log file path (auto-generated if not specified) |
| `-drv-log` | `""` | Driver log file path (auto-generated if not specified) |
| `-conn-log` | `""` | Connection events log file path (auto-generated if not specified) |
| `-driver` | `"..\_out\PortmasterKext_test.sys"` | Path to driver.sys |

**Note**: When console logging is disabled (`-console=false`) and no log file paths are specified, timestamped log files are automatically created in the `logs/` subdirectory with the format:
- `logs/app_2026-01-09_15-30-45.log`
- `logs/driver_2026-01-09_15-30-45.log`
- `logs/connections_2026-01-09_15-30-45.log`

### Examples

```powershell
# Basic usage with console output
.\playground.exe

# Log to files only (auto-generated timestamped files in logs/ folder)
.\playground.exe -console=false

# Log to specific files
.\playground.exe -console=false -app-log=my_app.log -drv-log=my_driver.log -conn-log=my_conn.log

# Log to both console and files
.\playground.exe -console=true -file=true

# Use specific driver path
.\playground.exe -driver "C:\path\to\driver.sys"
```

## Interactive Commands

| Command | Description |
|---------|-------------|
| `help` / `?` | Show help message |
| `status` | Show current driver status |
| `start [path]` | Start driver service (optionally specify driver path) |
| `stop` | Stop driver service |
| `redirect-start <IP>` | Start redirecting TCP/UDP through interface IP |
| `redirect-stop` | Stop traffic redirection |
| `logs-toggle` / `logs` | Toggle console logging on/off |
| `terminal-log-filter <regex>` | Set regex filter for terminal logs (empty to clear) |
| `exit` / `quit` / `q` | Exit the application |

### Keyboard Shortcuts

- **Ctrl+C**: Gracefully stop driver and exit
- **Enter** (empty input): Toggle console logging on/off (useful when logs are flooding)

### Log Filtering

Use `terminal-log-filter` to show only matching log lines in the console (file logs remain complete):

```
> terminal-log-filter ERROR
Terminal log filter set to regex: ERROR

> terminal-log-filter TCP|UDP
Terminal log filter set to regex: TCP|UDP

> terminal-log-filter +++++
Terminal log filter set to literal string: +++++

> terminal-log-filter
Terminal log filter cleared
```

**Features:**
- Supports regex patterns and literal strings
- If regex compilation fails, treats input as literal text (automatically escapes special chars)
- Filters apply to all loggers (app, driver, connection logs)
- Filtered messages are hidden from console but still written to log files

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                    playground.exe                           │
├─────────────────────────────────────────────────────────────┤
│  main.go          - Entry point, command loop               │
│  kext/            - Driver communication (protocol impl)    │
│  logger/          - Configurable logging with filtering     │
└─────────────────────────────────────────────────────────────┘
           │
           │ Windows Service Control + Device File I/O
           ▼
┌─────────────────────────────────────────────────────────────┐
│                  PortmasterKext_test.sys (Kernel)           │
│         \\.\PortmasterKext                                  │
└─────────────────────────────────────────────────────────────┘
```

## Protocol

The application communicates with the driver using a binary protocol:

### Commands (App → Driver)

| ID | Name | Description |
|----|------|-------------|
| 0 | Shutdown | Shutdown the driver |
| 1 | Verdict | Send verdict for a connection |
| 2 | UpdateV4 | Update IPv4 connection verdict |
| 3 | UpdateV6 | Update IPv6 connection verdict |
| 4 | ClearCache | Clear connection cache |
| 5 | GetLogs | Request buffered logs |

### Info Packets (Driver → App)

| ID | Type | Description |
|----|------|-------------|
| 0 | LogLine | Driver log message |
| 1 | ConnectionIpv4 | New IPv4 connection |
| 2 | ConnectionIpv6 | New IPv6 connection |
| 3 | ConnectionEndEventV4 | IPv4 connection ended |
| 4 | ConnectionEndEventV6 | IPv6 connection ended |

### Verdicts

| ID | Name | Description |
|----|------|-------------|
| 0 | Undecided | Connection pending decision |
| 2 | Accept | Accept (non-permanent) |
| 3 | PermanentAccept | Accept permanently |
| 4 | Block | Block (non-permanent) |
| 5 | PermanentBlock | Block permanently |
| 6 | Drop | Drop (non-permanent) |
| 7 | PermanentDrop | Drop permanently |
| 8 | RerouteToNameserver | Redirect to DNS port (53) |
| 9 | RerouteToTunnel | Redirect to tunnel port (717) |

## Behavior

1. **Default Mode**: All connections receive `PermanentAccept` verdict
2. **Redirect Mode** (`redirect-start <IP>`):
   - DNS traffic (port 53) → `PermanentAccept`
   - TCP/UDP traffic → `RerouteToTunnel`
   - Other protocols → `PermanentAccept`

## Troubleshooting

### "Failed to open service manager"
- Run as Administrator

### "Driver file not found"
- Check the driver path with `-driver` flag
- Build the driver first using `test\build_test.ps1`
- Default path is `test\_out\PortmasterKext_test.sys`

### "Failed to start driver service"
- Ensure test signing is enabled (`bcdedit -set TESTSIGNING ON`)
- Check the driver is properly signed
- Check Event Viewer for driver loading errors
- Verify certificate is installed in `PrivateCertStore`

### No connection events showing
- The driver may not have registered its callouts properly
- Check driver logs for errors
- Verify the driver version matches the expected protocol

## Module Information

- **Module name**: `playground`
- **Go version**: 1.22+
- **Dependencies**: `golang.org/x/sys`
