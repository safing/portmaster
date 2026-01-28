module playground

go 1.24.0

toolchain go1.24.7

require github.com/safing/portmaster/windows_kext/kextinterface v0.0.0

require golang.org/x/sys v0.38.0 // indirect

replace github.com/safing/portmaster/windows_kext/kextinterface => ../../kextinterface
