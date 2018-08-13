package process

func (m *Process) IsUser() bool {
	return m.UserID >= 1000
}

func (m *Process) IsAdmin() bool {
	return m.UserID >= 0
}

func (m *Process) IsSystem() bool {
	return m.UserID == 0
}
