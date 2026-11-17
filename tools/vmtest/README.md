# VM test suite

Runs the Go tests and an integration smoke test of `portmaster-core` in local
VMs: Linux (Fedora Cloud) as root, and Windows 11 as administrator with the
signed kernel extension. Everything runs as your user with QEMU/KVM and
user-mode networking; no root on the host is needed.

## Requirements

`qemu-system-x86_64`, `qemu-img`, `swtpm`, `xorriso`, `socat`, `rsync`,
OVMF (`/usr/share/edk2/ovmf`), read/write access to `/dev/kvm`, and Go.

VM disks, keys and results live in `~/vms/portmaster-test` (override with
`VMTEST_DIR`), never in the repository. The Windows test account's password
is generated at install time and stored in `windows/credentials` there.

## Set up (once)

```bash
# Linux: Fedora Cloud Base Generic qcow2 from https://fedoraproject.org/cloud/
tools/vmtest/linux/vm.sh create ~/vms/portmaster-test/images/Fedora-Cloud-Base-Generic-45-1.4.x86_64.qcow2

# Windows: any Windows 11 ISO. The answer file accepts the license terms and
# uses Microsoft's public generic key for Windows 11 Pro (stays unactivated).
tools/vmtest/windows/vm.sh create ~/Downloads/<windows-11>.iso
```

The Windows installation runs unattended for 30-60 minutes; watch it on VNC
at `127.0.0.1:5901`.

## Run

```bash
tools/vmtest/test-linux.sh          # unit tests (race detector) + smoke test, as root
tools/vmtest/test-windows.sh all    # unit tests + smoke test, as administrator
```

`test-linux.sh` resets the VM to its clean snapshot before every run. Both
print a summary and write logs to `~/vms/portmaster-test/results/`.

What the smoke tests check:

- the service starts with packet interception (nfqueue / kernel extension);
- DNS and HTTPS work, a domain blocked in `config.json` is blocked;
- the API answers, lists connections in netquery and denies unknown clients;
- 40 parallel connections do not break it;
- it shuts down cleanly and leaves a working network;
- Linux only: after `kill -9`, `recover-iptables` restores the network.

The Windows smoke test downloads the signed release binaries (including the
kernel extension) from Safing's update server with `portmaster-core update`
and runs the locally built `portmaster-core.exe` with them.

## Manage the VMs

```bash
tools/vmtest/linux/vm.sh   start|stop|status|ssh|reset
tools/vmtest/windows/vm.sh start|stop|status|ssh
```
