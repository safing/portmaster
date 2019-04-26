// Copyright Safing ICS Technologies GmbH. Use of this source code is governed by the AGPL license that can be found in the LICENSE file.

package process

import (
	"fmt"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	processInfo "github.com/shirou/gopsutil/process"

	"github.com/Safing/portbase/database/record"
	"github.com/Safing/portbase/log"
	"github.com/Safing/portmaster/profile"
)

// A Process represents a process running on the operating system
type Process struct {
	record.Base
	sync.Mutex

	UserID    int
	UserName  string
	UserHome  string
	Pid       int
	ParentPid int
	Path      string
	Cwd       string
	CmdLine   string
	FirstArg  string

	ExecName   string
	ExecHashes map[string]string
	// ExecOwner ...
	// ExecSignature ...

	UserProfileKey string
	profileSet     *profile.Set
	Name           string
	Icon           string
	// Icon is a path to the icon and is either prefixed "f:" for filepath, "d:" for database cache path or "c:"/"a:" for a the icon key to fetch it from a company / authoritative node and cache it in its own cache.

	FirstCommEstablished int64
	LastCommEstablished  int64
	CommCount            uint
	Virtual              bool // This process is merged into another process
}

// ProfileSet returns the assigned profile set.
func (p *Process) ProfileSet() *profile.Set {
	p.Lock()
	defer p.Unlock()

	return p.profileSet
}

// Strings returns a string represenation of process.
func (p *Process) String() string {
	p.Lock()
	defer p.Unlock()

	if p == nil {
		return "?"
	}
	return fmt.Sprintf("%s:%s:%d", p.UserName, p.Path, p.Pid)
}

// AddCommunication increases the connection counter and the last connection timestamp.
func (p *Process) AddCommunication() {
	p.Lock()
	defer p.Unlock()

	p.CommCount++
	p.LastCommEstablished = time.Now().Unix()
	if p.FirstCommEstablished == 0 {
		p.FirstCommEstablished = p.LastCommEstablished
	}
}

// RemoveCommunication lowers the connection counter by one.
func (p *Process) RemoveCommunication() {
	p.Lock()
	defer p.Unlock()

	if p.CommCount > 0 {
		p.CommCount--
	}
}

// GetOrFindPrimaryProcess returns the highest process in the tree that matches the given PID.
func GetOrFindPrimaryProcess(pid int) (*Process, error) {
	process, err := loadProcess(pid)
	if err != nil {
		return nil, err
	}

	for {
		parentProcess, err := loadProcess(process.ParentPid)
		if err != nil {
			log.Tracef("process: could not get parent (%d): %s", process.Pid, err)
			return process, nil
		}

		// parent process does not match, we reached the top of the tree of matching processes
		if process.Path != parentProcess.Path {
			// save to storage
			process.Save()
			// return primary process
			return process, nil
		}

		// mark as virtual
		process.Lock()
		process.Virtual = true
		process.Unlock()

		// save to storage
		process.Save()

		// continue up to process tree
		process = parentProcess
	}
}

// GetOrFindProcess returns the process for the given PID.
func GetOrFindProcess(pid int) (*Process, error) {
	p, err := loadProcess(pid)
	if err != nil {
		return nil, err
	}

	// save to storage
	p.Save()
	return p, nil
}

func loadProcess(pid int) (*Process, error) {
	process, ok := GetProcessFromStorage(pid)
	if ok {
		return process, nil
	}

	new := &Process{
		Pid: pid,
	}

	switch {
	case new.IsKernel():
		new.UserName = "Kernel"
		new.Name = "Operating System"
	default:

		pInfo, err := processInfo.NewProcess(int32(pid))
		if err != nil {
			return nil, err
		}

		// UID
		// net yet implemented for windows
		if runtime.GOOS == "linux" {
			var uids []int32
			uids, err = pInfo.Uids()
			if err != nil {
				log.Warningf("process: failed to get UID for p%d: %s", pid, err)
			} else {
				new.UserID = int(uids[0])
			}
		}

		// Username
		new.UserName, err = pInfo.Username()
		if err != nil {
			log.Warningf("process: failed to get Username for p%d: %s", pid, err)
		}

		// TODO: User Home
		// new.UserHome, err =

		// PPID
		ppid, err := pInfo.Ppid()
		if err != nil {
			log.Warningf("process: failed to get PPID for p%d: %s", pid, err)
		} else {
			new.ParentPid = int(ppid)
		}

		// Path
		new.Path, err = pInfo.Exe()
		if err != nil {
			log.Warningf("process: failed to get Path for p%d: %s", pid, err)
		}
		// Executable Name
		_, new.ExecName = filepath.Split(new.Path)

		// Current working directory
		// net yet implemented for windows
		// new.Cwd, err = pInfo.Cwd()
		// if err != nil {
		// 	log.Warningf("process: failed to get Cwd: %s", err)
		// }

		// Command line arguments
		new.CmdLine, err = pInfo.Cmdline()
		if err != nil {
			log.Warningf("process: failed to get Cmdline for p%d: %s", pid, err)
		}

		// Name
		new.Name, err = pInfo.Name()
		if err != nil {
			log.Warningf("process: failed to get Name for p%d: %s", pid, err)
		}
		if new.Name == "" {
			new.Name = new.ExecName
		}

		// TODO: App Icon
		// new.Icon, err =

		// get Profile
		// processPath := new.Path
		// var applyProfile *profiles.Profile
		// iterations := 0
		// for applyProfile == nil {
		//
		// 	iterations++
		// 	if iterations > 10 {
		// 		log.Warningf("process: got into loop while getting profile for %s", new)
		// 		break
		// 	}
		//
		// 	applyProfile, err = profiles.GetActiveProfileByPath(processPath)
		// 	if err == database.ErrNotFound {
		// 		applyProfile, err = profiles.FindProfileByPath(processPath, new.UserHome)
		// 	}
		// 	if err != nil {
		// 		log.Warningf("process: could not get profile for %s: %s", new, err)
		// 	} else if applyProfile == nil {
		// 		log.Warningf("process: no default profile found for %s", new)
		// 	} else {
		//
		// 		// TODO: there is a lot of undefined behaviour if chaining framework profiles
		//
		// 		// process framework
		// 		if applyProfile.Framework != nil {
		// 			if applyProfile.Framework.FindParent > 0 {
		// 				var ppid int32
		// 				for i := uint8(1); i < applyProfile.Framework.FindParent; i++ {
		// 					parent, err := pInfo.Parent()
		// 					if err != nil {
		// 						return nil, err
		// 					}
		// 					ppid = parent.Pid
		// 				}
		// 				if applyProfile.Framework.MergeWithParent {
		// 					return GetOrFindProcess(int(ppid))
		// 				}
		// 				// processPath, err = os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
		// 				// if err != nil {
		// 				// 	return nil, fmt.Errorf("could not read /proc/%d/exe: %s", pid, err)
		// 				// }
		// 				continue
		// 			}
		//
		// 			newCommand, err := applyProfile.Framework.GetNewPath(new.CmdLine, new.Cwd)
		// 			if err != nil {
		// 				return nil, err
		// 			}
		//
		// 			// assign
		// 			new.CmdLine = newCommand
		// 			new.Path = strings.SplitN(newCommand, " ", 2)[0]
		// 			processPath = new.Path
		//
		// 			// make sure we loop
		// 			applyProfile = nil
		// 			continue
		// 		}
		//
		// 		// apply profile to process
		// 		log.Debugf("process: applied profile to %s: %s", new, applyProfile)
		// 		new.Profile = applyProfile
		// 		new.ProfileKey = applyProfile.GetKey().String()
		//
		// 		// update Profile with Process icon if Profile does not have one
		// 		if !new.Profile.Default && new.Icon != "" && new.Profile.Icon == "" {
		// 			new.Profile.Icon = new.Icon
		// 			new.Profile.Save()
		// 		}
		// 	}
		// }
	}

	return new, nil
}
