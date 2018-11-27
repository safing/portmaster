package process

import "strings"

// IsUser returns whether the process is run by a normal user.
func (m *Process) IsUser() bool {
	return m.Pid != 4 && // Kernel
		!strings.HasPrefix(m.UserName, "NT-") // NT-Authority (localized!)
}

// IsAdmin returns whether the process is run by an admin user.
func (m *Process) IsAdmin() bool {
	return strings.HasPrefix(m.UserName, "NT-") // NT-Authority (localized!)
}

// IsSystem returns whether the process is run by the operating system.
func (m *Process) IsSystem() bool {
	return m.Pid == 4
}

// IsKernel returns whether the process is the Kernel.
func (m *Process) IsKernel() bool {
	return m.Pid == 4
}
