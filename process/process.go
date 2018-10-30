// Copyright Safing ICS Technologies GmbH. Use of this source code is governed by the AGPL license that can be found in the LICENSE file.

package process

import (
	"errors"
	"fmt"
	"runtime"
	"strconv"
	"sync"

	processInfo "github.com/shirou/gopsutil/process"

	"github.com/Safing/portbase/database/record"
	"github.com/Safing/portbase/log"
)

// A Process represents a process running on the operating system
type Process struct {
	record.Base
	sync.Mutex

	UserID     int
	UserName   string
	UserHome   string
	Pid        int
	ParentPid  int
	Path       string
	Cwd        string
	FileInfo   *FileInfo
	CmdLine    string
	FirstArg   string
	ProfileKey string
	Name       string
	Icon       string
	// Icon is a path to the icon and is either prefixed "f:" for filepath, "d:" for database cache path or "c:"/"a:" for a the icon key to fetch it from a company / authoritative node and cache it in its own cache.
}

// GetProcess fetches Process with the provided name from the default namespace.
func GetProcess(name string) (*Process, error) {
	return nil, errors.New("NIY")
}

func (m *Process) String() string {
	if m == nil {
		return "?"
	}
	return fmt.Sprintf("%s:%s:%d", m.UserName, m.Path, m.Pid)
}

func GetOrFindProcess(pid int) (*Process, error) {
	process, err := GetProcess(strconv.Itoa(pid))
	if err == nil {
		return process, nil
	}

	new := &Process{
		Pid: pid,
	}

	switch {
	case (pid == 0 && runtime.GOOS == "linux") || (pid == 4 && runtime.GOOS == "windows"):
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
			uids, err := pInfo.Uids()
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

		// get FileInfo
		new.FileInfo = GetFileInfo(new.Path)

	}

	// save to DB
	// new.Save()

	return new, nil
}
