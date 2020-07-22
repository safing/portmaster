//+build !windows,!linux

package process

// IsUser returns whether the process is run by a normal user.
func (p *Process) IsUser() bool {
	return true
}

// IsAdmin returns whether the process is run by an admin user.
func (p *Process) IsAdmin() bool {
	return false
}

// IsSystem returns whether the process is run by the operating system.
func (p *Process) IsSystem() bool {
	return false
}

// IsKernel returns whether the process is the Kernel.
func (p *Process) IsKernel() bool {
	return p.Pid == 0
}

// specialOSInit does special OS specific Process initialization.
func (p *Process) specialOSInit() {

}
