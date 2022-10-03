//go:build !windows && !linux
// +build !windows,!linux

package process

// SystemProcessID is the PID of the System/Kernel itself.
const SystemProcessID = 0
