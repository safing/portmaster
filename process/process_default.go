//+build !windows,!linux

package process

// IsKernel returns whether the process is the Kernel.
func (p *Process) IsKernel() bool {
	return p.Pid == 0
}

// specialOSInit does special OS specific Process initialization.
func (p *Process) specialOSInit() {

}
