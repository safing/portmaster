//go:build windows

package utils

import (
	"github.com/hectane/go-acl"
	"golang.org/x/sys/windows"
)

var (
	systemSID *windows.SID
	adminsSID *windows.SID
	usersSID  *windows.SID
)

func init() {
	// Initialize Security ID for all need groups.
	// Reference: https://learn.microsoft.com/en-us/windows-server/identity/ad-ds/manage/understand-security-identifiers
	var err error
	systemSID, err = windows.StringToSid("S-1-5-18") // SYSTEM (Local System)
	if err != nil {
		panic(err)
	}
	adminsSID, err = windows.StringToSid("S-1-5-32-544") // Administrators
	if err != nil {
		panic(err)
	}
	usersSID, err = windows.StringToSid("S-1-5-32-545") // Users
	if err != nil {
		panic(err)
	}
}

// SetDirPermission sets the permission of a directory.
func SetDirPermission(path string, perm FSPermission) error {
	SetFilePermission(path, perm)
	return nil
}

// SetExecPermission sets the permission of an executable file.
func SetExecPermission(path string, perm FSPermission) error {
	SetFilePermission(path, perm)
	return nil
}

// SetFilePermission sets the permission of a non executable file.
func SetFilePermission(path string, perm FSPermission) {
	switch perm {
	case AdminOnlyPermission:
		// Set only admin rights, remove all others.
		acl.Apply(
			path,
			true,
			false,
			acl.GrantSid(windows.GENERIC_ALL|windows.STANDARD_RIGHTS_ALL, systemSID),
			acl.GrantSid(windows.GENERIC_ALL|windows.STANDARD_RIGHTS_ALL, adminsSID),
		)
	case PublicReadPermission:
		// Set admin rights and read/execute rights for users, remove all others.
		acl.Apply(
			path,
			true,
			false,
			acl.GrantSid(windows.GENERIC_ALL|windows.STANDARD_RIGHTS_ALL, systemSID),
			acl.GrantSid(windows.GENERIC_ALL|windows.STANDARD_RIGHTS_ALL, adminsSID),
			acl.GrantSid(windows.GENERIC_READ|windows.GENERIC_EXECUTE, usersSID),
		)
	case PublicWritePermission:
		// Set full control to admin and regular users. Guest users will not have access.
		acl.Apply(
			path,
			true,
			false,
			acl.GrantSid(windows.GENERIC_ALL|windows.STANDARD_RIGHTS_ALL, systemSID),
			acl.GrantSid(windows.GENERIC_ALL|windows.STANDARD_RIGHTS_ALL, adminsSID),
			acl.GrantSid(windows.GENERIC_ALL|windows.STANDARD_RIGHTS_ALL, usersSID),
		)
	}
}
