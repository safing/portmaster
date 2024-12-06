//go:build windows

package utils

import (
	"github.com/hectane/go-acl"
	"golang.org/x/sys/windows"
)

func SetDirPermission(path string, perm FSPermission) error {
	setWindowsFilePermissions(path, perm)
	return nil
}

// SetExecPermission sets the permission of an executable file.
func SetExecPermission(path string, perm FSPermission) error {
	return SetDirPermission(path, perm)
}

func setWindowsFilePermissions(path string, perm FSPermission) {
	switch perm {
	case AdminOnlyPermission:
		// Set only admin rights, remove all others.
		acl.Apply(path, true, false, acl.GrantName(windows.GENERIC_ALL|windows.STANDARD_RIGHTS_ALL, "Administrators"))
	case PublicReadPermission:
		// Set admin rights and read/execute rights for users, remove all others.
		acl.Apply(path, true, false, acl.GrantName(windows.GENERIC_ALL|windows.STANDARD_RIGHTS_ALL, "Administrators"))
		acl.Apply(path, false, false, acl.GrantName(windows.GENERIC_EXECUTE, "Users"))
		acl.Apply(path, false, false, acl.GrantName(windows.GENERIC_READ, "Users"))
	case PublicWritePermission:
		// Set full control to admin and regular users. Guest users will not have access.
		acl.Apply(path, true, false, acl.GrantName(windows.GENERIC_ALL|windows.STANDARD_RIGHTS_ALL, "Administrators"))
		acl.Apply(path, false, false, acl.GrantName(windows.GENERIC_ALL|windows.STANDARD_RIGHTS_ALL, "Users"))
	}
}
