# Portmaster (Rust Core & Unlocked Edition)

[![CI](https://github.com/VLOD-ZDOV/portmaster-rust/actions/workflows/ci.yml/badge.svg)](https://github.com/VLOD-ZDOV/portmaster-rust/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/VLOD-ZDOV/portmaster-rust?include_prereleases)](https://github.com/VLOD-ZDOV/portmaster-rust/releases/latest)
[![License](https://img.shields.io/badge/license-GPL--3.0-blue.svg)](LICENSE)

An advanced, privacy-first application firewall and traffic controller for Linux and Windows.

This fork modernizes Portmaster by introducing a high-performance **Rust Core** (`pm-core`), unlocking all features locally with zero external account dependencies, and integrating the **Xray proxy engine** for application-level split tunneling and circumvention.

---

## Key Features & Highlights

- **Pure Rust Core (`portmaster-core-rs`)**:
  Rewritten from the ground up in Rust (`crates/pm-*`) for minimal memory footprint, blazing speed, and memory safety. Both the new Rust core and the legacy Go core are supported.
- **Integrated Xray Proxy Engine (`pm-xray` & `pm-splittun`)**:
  Route individual applications, domains, or traffic scopes through your own VLESS (Reality/Vision), Shadowsocks, Trojan, or SOCKS5 proxies without needing external TUN adapters.
- **100% Unlocked & Local**:
  No Safing account, sign-in, or subscription required. Network history recording, per-app bandwidth accounting, and advanced filter rules operate completely offline.
- **Cross-Platform Interception**:
  - **Linux**: Intercepts packets via `NFQUEUE` / `iptables` / `nftables` with kernel process resolution via `/proc`.
  - **Windows**: Deep packet inspection and connection verdict handling via the Windows Filtering Platform (WFP) kernel driver (`PortmasterKext.sys`) and IP Helper API.
- **Encrypted DNS**:
  Built-in DNS proxy supporting DNS-over-TLS (DoT) and DNS-over-HTTPS (DoH) with split-horizon routing and blocklist enforcement.
- **Modern UI Options**:
  - Full-featured Angular desktop UI with unlocked settings and proxy routing controls.
  - Experimental ultra-lightweight Leptos/WASM web dashboard (`pilot/leptos/web`).

---

## Quick Installation & Patcher

If you already have Portmaster installed or want to switch your installation to the Rust Core and modern UI, run the one-liner for your platform:

### Linux (One-Liner)

```bash
curl -fsSL https://raw.githubusercontent.com/VLOD-ZDOV/portmaster-rust/main/scripts/patch-portmaster.sh | sudo bash
```

*Options:*
- Switch to Go core: `sudo ./scripts/patch-portmaster.sh --core go`
- Restore previous installation: `sudo ./scripts/patch-portmaster.sh --restore`

### Windows (PowerShell Administrator)

```powershell
irm https://raw.githubusercontent.com/VLOD-ZDOV/portmaster-rust/main/scripts/patch-portmaster.ps1 | iex
```

*Options:*
- Switch to Go core: `.\scripts\patch-portmaster.ps1 -Core go`
- Restore previous installation: `.\scripts\patch-portmaster.ps1 -Restore`

---

## Architecture Overview

```
                      ┌──────────────────────────────────────┐
                      │          Portmaster UI               │
                      │  (Angular / Tauri / Leptos Pilot)    │
                      └──────────────────┬───────────────────┘
                                         │ REST / WebSocket API
                                         ▼
                      ┌──────────────────────────────────────┐
                      │    Portmaster Core (Rust / Go)       │
                      │ ┌──────────────┐    ┌──────────────┐ │
                      │ │  pm-firewall │    │  pm-resolver │ │
                      │ └──────┬───────┘    └──────────────┘ │
                      │        │                             │
                      │ ┌──────▼───────┐    ┌──────────────┐ │
                      │ │ pm-splittun  │───►│   pm-xray    │ │
                      │ └──────────────┘    └──────┬───────┘ │
                      └─────────┬──────────────────┼─────────┘
                                │                  │
         ┌──────────────────────┴────────┐         │ Upstream Proxy
         ▼                               ▼         ▼ (VLESS / SS / Trojan)
 ┌──────────────┐                 ┌──────────────┐
 │ Linux Filter │                 │ Windows WFP  │
 │ (NFQUEUE)    │                 │ (KEXT Driver)│
 └──────────────┘                 └──────────────┘
```

### Workspace Crates

| Crate | Description |
|---|---|
| `pm-core` | Application entry point, service manager, and lifecycle orchestration |
| `pm-firewall` | Connection tracking, verdict pipeline, and rule evaluation engine |
| `pm-splittun` | Split tunneling controller directing traffic between local filter and proxy |
| `pm-xray` | Embedded Xray proxy client runtime and configuration manager |
| `pm-resolver` | Secure DNS engine supporting DoH, DoT, blocklists, and caching |
| `pm-winkext` | Windows Filtering Platform (WFP) kernel extension driver interface |
| `pm-network` | Process detection, connection state table, and socket mapper |
| `pm-database` | Local SQLite database storage for connections, history, and metrics |

---

## Building from Source

### Prerequisites

- **Rust**: 1.80+ (`rustup default stable`)
- **Go**: 1.22+ (if building the legacy Go core)
- **Node.js**: 18+ & `npm` (for the Angular desktop UI)

### Build Rust Core

```bash
# Build native release binary
cargo build --release -p pm-core --bin portmaster-core-rs

# Result binary:
# Linux:   target/release/portmaster-core-rs
# Windows: target/release/portmaster-core-rs.exe
```

### Build UI Bundle

```bash
cd desktop/angular
npm ci --legacy-peer-deps
npm run build-libs
npx ng build --configuration production --base-href /ui/modules/portmaster/ portmaster

# Pack bundle
cd dist && zip -r ../../../portmaster.zip .
```

---

## Switching Between Cores on Linux

The system service can be toggled at any time using the bundled switcher:

```bash
# Check active core
sudo ./packaging/linux/portmaster-core-switch.sh status

# Switch to Rust core
sudo ./packaging/linux/portmaster-core-switch.sh rust

# Switch to Go core
sudo ./packaging/linux/portmaster-core-switch.sh go
```

---

## License

This project is licensed under the [GPL-3.0 License](LICENSE).
Portions derived from the upstream project are Copyright (c) Safing ICS Technologies GmbH.
All modifications and Rust core implementation are released under the same open-source terms.
