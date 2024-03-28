package terminal

// Permission is a bit-map of granted permissions.
type Permission uint16

// Permissions.
const (
	NoPermission      Permission = 0x0
	MayExpand         Permission = 0x1
	MayConnect        Permission = 0x2
	IsHubOwner        Permission = 0x100
	IsHubAdvisor      Permission = 0x200
	IsCraneController Permission = 0x8000
)

// AuthorizingTerminal is an interface for terminals that support authorization.
type AuthorizingTerminal interface {
	GrantPermission(grant Permission)
	HasPermission(required Permission) bool
}

// GrantPermission grants the specified permissions to the Terminal.
func (t *TerminalBase) GrantPermission(grant Permission) {
	t.lock.Lock()
	defer t.lock.Unlock()

	t.permission |= grant
}

// HasPermission returns if the Terminal has the specified permission.
func (t *TerminalBase) HasPermission(required Permission) bool {
	t.lock.RLock()
	defer t.lock.RUnlock()

	return t.permission.Has(required)
}

// Has returns if the permission includes the specified permission.
func (p Permission) Has(required Permission) bool {
	return p&required == required
}

// AddPermissions combines multiple permissions.
func AddPermissions(perms ...Permission) Permission {
	var all Permission
	for _, p := range perms {
		all |= p
	}
	return all
}
