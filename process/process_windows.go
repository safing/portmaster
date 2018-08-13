package process

import "strings"

func (m *Process) IsUser() bool {
	return m.Pid != 4 && // Kernel
		!strings.HasPrefix(m.UserName, "NT-") // NT-Authority (localized!)
}

func (m *Process) IsAdmin() bool {
	return strings.HasPrefix(m.UserName, "NT-") // NT-Authority (localized!)
}

func (m *Process) IsSystem() bool {
	return m.Pid == 4
}
