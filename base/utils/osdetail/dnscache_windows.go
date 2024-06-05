package osdetail

import (
	"os/exec"
)

// EnableDNSCache enables the Windows Service "DNS Client" by setting the registry value "HKEY_LOCAL_MACHINE\SYSTEM\CurrentControlSet\services\Dnscache" to 2 (Automatic).
// A reboot is required for this setting to take effect.
func EnableDNSCache() error {
	return exec.Command("reg", "add", "HKEY_LOCAL_MACHINE\\SYSTEM\\CurrentControlSet\\services\\Dnscache", "/v", "Start", "/t", "REG_DWORD", "/d", "2", "/f").Run()
}

// DisableDNSCache disables the Windows Service "DNS Client" by setting the registry value "HKEY_LOCAL_MACHINE\SYSTEM\CurrentControlSet\services\Dnscache" to 4 (Disabled).
// A reboot is required for this setting to take effect.
func DisableDNSCache() error {
	return exec.Command("reg", "add", "HKEY_LOCAL_MACHINE\\SYSTEM\\CurrentControlSet\\services\\Dnscache", "/v", "Start", "/t", "REG_DWORD", "/d", "4", "/f").Run()
}
