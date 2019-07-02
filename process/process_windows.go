package process

import (
	"fmt"
	"strings"

	"github.com/safing/portbase/log"
	"github.com/safing/portbase/utils/osdetail"
)

// IsUser returns whether the process is run by a normal user.
func (p *Process) IsUser() bool {
	return p.Pid != 4 && // Kernel
		!strings.HasPrefix(p.UserName, "NT") // NT-Authority (localized!)
}

// IsAdmin returns whether the process is run by an admin user.
func (p *Process) IsAdmin() bool {
	return strings.HasPrefix(p.UserName, "NT") // NT-Authority (localized!)
}

// IsSystem returns whether the process is run by the operating system.
func (p *Process) IsSystem() bool {
	return p.Pid == 4
}

// IsKernel returns whether the process is the Kernel.
func (p *Process) IsKernel() bool {
	return p.Pid == 4
}

// specialOSInit does special OS specific Process initialization.
func (p *Process) specialOSInit() {
	// add svchost.exe service names to Name
	if p.ExecName == "svchost.exe" {
		svcNames, err := osdetail.GetServiceNames(int32(p.Pid))
		switch err {
		case nil:
			p.Name += fmt.Sprintf(" (%s)", svcNames)
		case osdetail.ErrServiceNotFound:
			log.Tracef("process: failed to get service name for svchost.exe (pid %d): %s", p.Pid, err)
		default:
			log.Warningf("process: failed to get service name for svchost.exe (pid %d): %s", p.Pid, err)
		}
	}
}
