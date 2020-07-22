package process

import (
	"fmt"

	"github.com/safing/portbase/log"
	"github.com/safing/portbase/utils/osdetail"
)

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
