// Copyright Safing ICS Technologies GmbH. Use of this source code is governed by the AGPL license that can be found in the LICENSE file.

package process

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	processInfo "github.com/shirou/gopsutil/process"

	"github.com/safing/portbase/database/record"
	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/profile"
)

var (
	dupReqMap  = make(map[int]*sync.Mutex)
	dupReqLock sync.Mutex
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

	Virtual bool   // This process is either merged into another process or is not needed.
	Error   string // If this is set, the process is invalid. This is used to cache failing or inexistent processes.
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

	// check if we should save
	save := false
	if p.LastCommEstablished < time.Now().Add(-3*time.Second).Unix() {
		save = true
	}

	// update LastCommEstablished
	p.LastCommEstablished = time.Now().Unix()
	if p.FirstCommEstablished == 0 {
		p.FirstCommEstablished = p.LastCommEstablished
	}

	if save {
		go p.Save()
	}
}

// var db = database.NewInterface(nil)

// CountConnections returns the count of connections of a process
// func (p *Process) CountConnections() int {
// 	q, err := query.New(fmt.Sprintf("%s/%d/", processDatabaseNamespace, p.Pid)).
// 		Where(query.Where("Pid", query.Exists, nil)).
// 		Check()
// 	if err != nil {
// 		log.Warningf("process: failed to build query to get connection count of process: %s", err)
// 		return -1
// 	}
//
// 	it, err := db.Query(q)
// 	if err != nil {
// 		log.Warningf("process: failed to query db to get connection count of process: %s", err)
// 		return -1
// 	}
//
// 	cnt := 0
// 	for _ = range it.Next {
// 		cnt++
// 	}
// 	if it.Err() != nil {
// 		log.Warningf("process: failed to query db to get connection count of process: %s", err)
// 		return -1
// 	}
//
// 	return cnt
// }

// GetOrFindPrimaryProcess returns the highest process in the tree that matches the given PID.
func GetOrFindPrimaryProcess(ctx context.Context, pid int) (*Process, error) {
	log.Tracer(ctx).Tracef("process: getting primary process for PID %d", pid)

	if pid == -1 {
		return UnknownProcess, nil
	}
	if pid == 0 {
		return OSProcess, nil
	}

	process, err := loadProcess(ctx, pid)
	if err != nil {
		return nil, err
	}
	if process.Error != "" {
		return nil, fmt.Errorf("%s [cached error]", process.Error)
	}

	for {
		if process.ParentPid == 0 {
			return OSProcess, nil
		}
		parentProcess, err := loadProcess(ctx, process.ParentPid)
		if err != nil {
			log.Tracer(ctx).Tracef("process: could not get parent of %d: %d: %s", process.Pid, process.ParentPid, err)
			return process, nil
		}
		if parentProcess.Error != "" {
			log.Tracer(ctx).Tracef("process: could not get parent of %d: %d: %s [cached error]", process.Pid, process.ParentPid, parentProcess.Error)
			return process, nil
		}

		// if parent process path does not match, we have reached the top of the tree of matching processes
		if process.Path != parentProcess.Path {
			// found primary process

			// mark for use, save to storage
			process.Lock()
			if process.Virtual {
				process.Virtual = false
				go process.Save()
			}
			process.Unlock()

			return process, nil
		}

		// continue up to process tree
		process = parentProcess
	}
}

// GetOrFindProcess returns the process for the given PID.
func GetOrFindProcess(ctx context.Context, pid int) (*Process, error) {
	log.Tracer(ctx).Tracef("process: getting process for PID %d", pid)

	if pid == -1 {
		return UnknownProcess, nil
	}
	if pid == 0 {
		return OSProcess, nil
	}

	p, err := loadProcess(ctx, pid)
	if err != nil {
		return nil, err
	}
	if p.Error != "" {
		return nil, fmt.Errorf("%s [cached error]", p.Error)
	}

	// mark for use, save to storage
	p.Lock()
	if p.Virtual {
		p.Virtual = false
		go p.Save()
	}
	p.Unlock()
	return p, nil
}

func loadProcess(ctx context.Context, pid int) (*Process, error) {
	if pid == -1 {
		return UnknownProcess, nil
	}
	if pid == 0 {
		return OSProcess, nil
	}

	process, ok := GetProcessFromStorage(pid)
	if ok {
		return process, nil
	}

	// dedup requests
	dupReqLock.Lock()
	mutex, requestActive := dupReqMap[pid]
	if !requestActive {
		mutex = new(sync.Mutex)
		mutex.Lock()
		dupReqMap[pid] = mutex
		dupReqLock.Unlock()
	} else {
		dupReqLock.Unlock()
		log.Tracer(ctx).Tracef("process: waiting for duplicate request for PID %d to complete", pid)
		mutex.Lock()
		// wait until duplicate request is finished, then fetch current Process and return
		mutex.Unlock()
		process, ok = GetProcessFromStorage(pid)
		if ok {
			return process, nil
		}
		return nil, fmt.Errorf("previous request for process with PID %d failed", pid)
	}

	// lock request for this pid
	defer func() {
		dupReqLock.Lock()
		delete(dupReqMap, pid)
		dupReqLock.Unlock()
		mutex.Unlock()
	}()

	// create new process
	new := &Process{
		Pid:     pid,
		Virtual: true, // caller must decide to actually use the process - we need to save now.
	}

	switch {
	case new.IsKernel():
		new.UserName = "Kernel"
		new.Name = "Operating System"
	default:

		pInfo, err := processInfo.NewProcess(int32(pid))
		if err != nil {
			// TODO: remove this workaround as soon as NewProcess really returns an error on windows when the process does not exist
			// Issue: https://github.com/shirou/gopsutil/issues/729
			_, err = pInfo.Name()
			if err != nil {
				// process does not exists
				return nil, err
			}
		}

		// UID
		// net yet implemented for windows
		if runtime.GOOS == "linux" {
			var uids []int32
			uids, err = pInfo.Uids()
			if err != nil {
				return failedToLoad(new, fmt.Errorf("failed to get UID for p%d: %s", pid, err))
			}
			new.UserID = int(uids[0])
		}

		// Username
		new.UserName, err = pInfo.Username()
		if err != nil {
			return failedToLoad(new, fmt.Errorf("process: failed to get Username for p%d: %s", pid, err))
		}

		// TODO: User Home
		// new.UserHome, err =

		// PPID
		ppid, err := pInfo.Ppid()
		if err != nil {
			return failedToLoad(new, fmt.Errorf("failed to get PPID for p%d: %s", pid, err))
		}
		new.ParentPid = int(ppid)

		// Path
		new.Path, err = pInfo.Exe()
		if err != nil {
			return failedToLoad(new, fmt.Errorf("failed to get Path for p%d: %s", pid, err))
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
			return failedToLoad(new, fmt.Errorf("failed to get Cmdline for p%d: %s", pid, err))
		}

		// Name
		new.Name, err = pInfo.Name()
		if err != nil {
			return failedToLoad(new, fmt.Errorf("failed to get Name for p%d: %s", pid, err))
		}
		if new.Name == "" {
			new.Name = new.ExecName
		}

		// OS specifics
		new.specialOSInit()

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

	new.Save()
	return new, nil
}

func failedToLoad(p *Process, err error) (*Process, error) {
	p.Error = err.Error()
	p.Save()
	return nil, err
}
