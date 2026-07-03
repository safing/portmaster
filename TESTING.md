# Testing Portmaster

This page documents ways to test if Portmaster works as intended.

⚠ Work in Progress. Currently we are just collecting helpful things we find.

## Websites for Testing:

- <http://icmpcheck.popcount.org/>: Check 
  - ICMP path MTU packet delivery
  - IP fragmented packet delivery

## Troubleshooting

### Tauri window shows "files corrupted" on first launch

The Portmaster daemon verifies the binaries in its `--bin-dir` against the
hashes in `<bin-dir>/index.json`. If the index is missing or its hashes do
not match the bundled binaries, the daemon reports:

> Portmaster has detected that one or more of its own files have been
> corrupted. Please re-install the software.

Common causes on any install where `BinDir` does not resolve to where
the binaries actually live (merged-/usr layouts, custom prefixes,
containerized installs, hand-symlinked paths):

1. **Missing `index.json`** — the Linux installer RPM/DEB bundle must
   contain a binary update index alongside `portmaster-core`,
   `portmaster.zip`, and `assets.zip`. If your packager script
   re-creates the bundle by hand, generate the index with the
   `updatemgr scan` subcommand from this repository:

   ```
   updatemgr scan --dir /path/to/binary > /path/to/binary/index.json
   ```

2. **Symlinked `BinDir`** — when any component of the `BinDir` path
   itself is a symlink, the daemon's index loader and firewall
   authenticator can disagree on the canonical location. Pass the
   resolved path to `--bin-dir` explicitly, or upgrade to a build that
   resolves symlinks internally (see
   `service/config.go::ServiceConfig.Init`).

3. **Out-of-band re-install** — replacing the bundled binaries via a
   package manager upgrade without rebuilding `index.json` produces a
   hash mismatch. Re-run `updatemgr scan` or let the Portmaster updater
   handle upgrades end-to-end.

### Tauri window shows "trusted root is /usr/lib/portmaster" / 403 Forbidden

The daemon's HTTP API is restricted to binaries whose resolved path lives
inside `<bin-dir>` (the daemon's "trusted root"). The Tauri GUI spawns a
webview that connects to the daemon from the GUI's own install path,
which is usually *not* inside `<bin-dir>`. To allow the GUI to talk to
the daemon, add the resolved path of the Tauri binary to the daemon's
`--allowed-clients` flag:

```
ExecStart=/usr/bin/portmaster-core \
    --bin-dir=/usr/lib/portmaster \
    --data-dir=/var/lib/portmaster \
    --allowed-clients=/usr/bin/portmaster-gui
```

The path passed to `--allowed-clients` must be the resolved (symlink-free)
path of the binary that opens the connection.
