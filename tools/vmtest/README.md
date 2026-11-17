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

`test-linux.sh` resets the VM to its clean snapshot before every run.

A soak test runs the service for a longer time under constant traffic
(HTTPS and DNS lookups of always new names) and samples memory, goroutines,
open files and data size every minute. It fails if one of them keeps growing
well beyond the level after warm-up:

```bash
tools/vmtest/test-linux.sh soak 60   # minutes
``` Both
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

## Run on Proxmox VE

The same VMs can run on a Proxmox VE host instead of local QEMU. Set
`VMTEST_BACKEND=proxmox` for every command:

```bash
VMTEST_BACKEND=proxmox tools/vmtest/test-linux.sh
VMTEST_BACKEND=proxmox tools/vmtest/test-windows.sh all
VMTEST_BACKEND=proxmox tools/vmtest/linux/vm.sh status
```

Tests are still compiled on this machine; only the VMs move. The suite
reaches the host with `ssh proxmox` (root, key login; override with
`VMTEST_PVE_HOST`) and starts, stops and resets the VMs with `qm`. The VMs are
on the host's LAN bridge `vmbr0` with DHCP. The suite finds their address
from the MAC address on the host and connects through the host (ProxyJump),
so they need not be routable from here. The Linux smoke test reads the serial
console from the host's socket instead of a local file.

| VM | VMID | Hardware |
|----|------|----------|
| `pm-vmtest-linux` | 900 (`VMTEST_LINUX_VMID`) | SeaBIOS, virtio disk and NIC, serial socket, snapshot `clean` |
| `pm-vmtest-windows` | 901 (`VMTEST_WINDOWS_VMID`) | OVMF with Secure Boot (MS keys), TPM 2.0, SATA disk, e1000e NIC |

The Proxmox VMs were imported from the local ones rather than installed anew:

```bash
# local: Linux from its clean snapshot, both flattened and compressed
qemu-img convert -O qcow2 -c -o compression_type=zstd -l snapshot.name=clean linux/disk.qcow2 linux.qcow2
qemu-img convert -O qcow2 -c -o compression_type=zstd windows/disk.qcow2 windows.qcow2
# host: create the VMs (see `qm config 900` / `qm config 901`), then
qm disk import 900 linux.qcow2 ssd-thin   && qm set 900 --virtio0 ssd-thin:vm-900-disk-0 --boot order=virtio0
qm disk import 901 windows.qcow2 ssd-thin && qm set 901 --sata0 ssd-thin:vm-901-disk-2 --boot order=sata0
qm snapshot 900 clean
```

`vm.sh create` is not available on Proxmox. The Windows test account's
password stays in `~/vms/portmaster-test/windows/credentials`; the Windows
screen is in the Proxmox web UI (console of VM 901).

## Manage the VMs

```bash
tools/vmtest/linux/vm.sh   start|stop|status|ssh|reset
tools/vmtest/windows/vm.sh start|stop|status|ssh
```
