// Copyright Safing ICS Technologies GmbH. Use of this source code is governed by the AGPL license that can be found in the LICENSE file.

package process

import (
	"fmt"
	"runtime"
	"strings"
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

	FirstConnectionEstablished int64
	LastConnectionEstablished  int64
	ConnectionCount            uint
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

// AddConnection increases the connection counter and the last connection timestamp.
func (p *Process) AddConnection() {
	p.Lock()
	defer p.Unlock()

	p.ConnectionCount++
	p.LastConnectionEstablished = time.Now().Unix()
	if p.FirstConnectionEstablished == 0 {
		p.FirstConnectionEstablished = p.LastConnectionEstablished
	}
}

// RemoveConnection lowers the connection counter by one.
func (p *Process) RemoveConnection() {
	p.Lock()
	defer p.Unlock()

	if p.ConnectionCount > 0 {
		p.ConnectionCount--
	}
}

// GetOrFindProcess returns the process for the given PID.
func GetOrFindProcess(pid int) (*Process, error) {
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
				log.Warningf("process: failed to get UID: %s", err)
			} else {
				new.UserID = int(uids[0])
			}
		}

		// Username
		new.UserName, err = pInfo.Username()
		if err != nil {
			log.Warningf("process: failed to get Username: %s", err)
		}

		// TODO: User Home
		// new.UserHome, err =

		// PPID
		ppid, err := pInfo.Ppid()
		if err != nil {
			log.Warningf("process: failed to get PPID: %s", err)
		} else {
			new.ParentPid = int(ppid)
		}

		// Path
		new.Path, err = pInfo.Exe()
		if err != nil {
			log.Warningf("process: failed to get Path: %s", err)
		}

		// Current working directory
		// net yet implemented for windows
		// new.Cwd, err = pInfo.Cwd()
		// if err != nil {
		// 	log.Warningf("process: failed to get Cwd: %s", err)
		// }

		// Command line arguments
		new.CmdLine, err = pInfo.Cmdline()
		if err != nil {
			log.Warningf("process: failed to get Cmdline: %s", err)
		}

		// Name
		new.Name, err = pInfo.Name()
		if err != nil {
			log.Warningf("process: failed to get Name: %s", err)
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

		// Executable Information

		// FIXME: use os specific path seperator
		splittedPath := strings.Split(new.Path, "/")
		new.ExecName = splittedPath[len(splittedPath)-1]
	}

	// save to storage
	new.Save()

	return new, nil
}
