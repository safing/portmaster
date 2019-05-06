package process

// IsUser returns whether the process is run by a normal user.
func (m *Process) IsUser() bool {
	return m.UserID >= 1000
}

// IsAdmin returns whether the process is run by an admin user.
func (m *Process) IsAdmin() bool {
	return m.UserID >= 0
}

// IsSystem returns whether the process is run by the operating system.
func (m *Process) IsSystem() bool {
	return m.UserID == 0
}

// IsKernel returns whether the process is the Kernel.
func (m *Process) IsKernel() bool {
	return m.Pid == 0
}

// specialOSInit does special OS specific Process initialization.
func (m *Process) specialOSInit() {

}
