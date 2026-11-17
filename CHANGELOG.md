# Changelog — portmasterpro

All notable changes to this fork relative to upstream [safing/portmaster](https://github.com/safing/portmaster).
Format: [Keep a Changelog](https://keepachangelog.com/en/1.1.0/). Versions follow SemVer; pre-releases carry a `-rcN` suffix.

## [Unreleased]

### Changed
- Merged upstream `development` up to `21a2b064` (2.2.3 release line: updates path-traversal fix,
  profile lookup/geo-rule fixes, iptables crash-safety, UI fixes, tray icon OS theme).
- Release workflow now creates the GitHub release automatically on `v*` tag push (pre-release
  when the tag contains `-`), with notes taken from this changelog.

### Added
- `CHANGELOG.md`, `RELEASE.md` (release checklist) and a scheduled upstream-sync workflow that
  opens a pull request when upstream `development` has new commits.

## [0.1.0-rc1] - 2026-10-01

First public release candidate.

### Removed
- SPN (Safing Privacy Network), Safing account/login, subscription and feature gating.
- Telemetry / cloud sync (`service/sync` is a no-op).
- SPN navigation, `/spn` route, login/subscribe header, upsell widgets and "Portmaster Plus/Pro"
  copy in the Angular UI; SPN entries in the tray menu.

### Changed
- Network History and Bandwidth Visibility are available locally for everyone; history follows the
  per-app setting.
- Upstream **software** update checks are opt-in (`core/automaticUpdates=false`): upstream
  binaries would replace this build. Intelligence data updates (filter lists, GeoIP) stay enabled.
- `spn/access` reduced to an offline compatibility shim so remaining SPN packages compile but are
  never started.

### Added
- `no-telemetry-ci.yml`: full Go build/vet/test, race checks, Angular production build and guards
  against account/telemetry endpoints in Go sources and the built UI bundle.
- Linux cross-build of the Windows NSIS installer (`packaging/windows/render_nsis_linux.py`) and the
  `release-windows-installer.yml` workflow.
- `BUILDING.md` with the full build, test and installer flow.

### Security
- No connection to `account.safing.io`; the only remaining upstream endpoint is
  `updates.safing.io` for intelligence data (and, if enabled, binary update checks).
- Installer and binaries are unsigned; the kernel driver is Safing's signed stable driver (2.2.3).
