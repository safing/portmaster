//+build !windows,!linux

package process

// SystemProcessID is the PID of the System/Kernel itself.
const SystemProcessID = 0

// specialOSInit does special OS specific Process initialization.
func (p *Process) specialOSInit() {

}
