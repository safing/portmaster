package process

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	processInfo "github.com/shirou/gopsutil/process"

	"github.com/safing/portbase/database/record"
	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/profile"
)

const (
	onLinux = runtime.GOOS == "linux"
)

var (
	dupReqMap  = make(map[int]*sync.WaitGroup)
	dupReqLock sync.Mutex
)

// A Process represents a process running on the operating system
type Process struct {
	record.Base
	sync.Mutex

	// Constant attributes.

	Name      string
	UserID    int
	UserName  string
	UserHome  string
	Pid       int
	ParentPid int
	Path      string
	ExecName  string
	Cwd       string
	CmdLine   string
	FirstArg  string

	LocalProfileKey string
	profile         *profile.LayeredProfile

	// Mutable attributes.

	FirstSeen int64
	LastSeen  int64
	Virtual   bool   // This process is either merged into another process or is not needed.
	Error     string // Cache errors

	ExecHashes map[string]string
}

// Profile returns the assigned layered profile.
func (p *Process) Profile() *profile.LayeredProfile {
	return p.profile
}

// Strings returns a string representation of process.
func (p *Process) String() string {
	if p == nil {
		return "?"
	}

	return fmt.Sprintf("%s:%s:%d", p.UserName, p.Path, p.Pid)
}

// GetOrFindPrimaryProcess returns the highest process in the tree that matches the given PID.
func GetOrFindPrimaryProcess(ctx context.Context, pid int) (*Process, error) {
	log.Tracer(ctx).Tracef("process: getting primary process for PID %d", pid)

	switch pid {
	case UnidentifiedProcessID:
		return GetUnidentifiedProcess(ctx), nil
	case SystemProcessID:
		return GetSystemProcess(ctx), nil
	}

	process, err := loadProcess(ctx, pid)
	if err != nil {
		return nil, err
	}

	for {
		if process.ParentPid <= 0 {
			return process, nil
		}
		parentProcess, err := loadProcess(ctx, process.ParentPid)
		if err != nil {
			log.Tracer(ctx).Tracef("process: could not get parent of %d: %d: %s", process.Pid, process.ParentPid, err)
			saveFailedProcess(process.ParentPid, err.Error())
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

	switch pid {
	case UnidentifiedProcessID:
		return GetUnidentifiedProcess(ctx), nil
	case SystemProcessID:
		return GetSystemProcess(ctx), nil
	}

	p, err := loadProcess(ctx, pid)
	if err != nil {
		return nil, err
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

func deduplicateRequest(ctx context.Context, pid int) (finishRequest func()) {
	dupReqLock.Lock()

	// get  duplicate request waitgroup
	wg, requestActive := dupReqMap[pid]

	// someone else is already on it!
	if requestActive {
		dupReqLock.Unlock()

		// log that we are waiting
		log.Tracer(ctx).Tracef("intel: waiting for duplicate request for PID %d to complete", pid)
		// wait
		wg.Wait()
		// done!
		return nil
	}

	// we are currently the only one doing a request for this

	// create new waitgroup
	wg = new(sync.WaitGroup)
	// add worker (us!)
	wg.Add(1)
	// add to registry
	dupReqMap[pid] = wg

	dupReqLock.Unlock()

	// return function to mark request as finished
	return func() {
		dupReqLock.Lock()
		defer dupReqLock.Unlock()
		// mark request as done
		wg.Done()
		// delete from registry
		delete(dupReqMap, pid)
	}
}

func loadProcess(ctx context.Context, pid int) (*Process, error) {

	switch pid {
	case UnidentifiedProcessID:
		return GetUnidentifiedProcess(ctx), nil
	case SystemProcessID:
		return GetSystemProcess(ctx), nil
	}

	process, ok := GetProcessFromStorage(pid)
	if ok {
		return process, nil
	}

	// dedupe!
	markRequestFinished := deduplicateRequest(ctx, pid)
	if markRequestFinished == nil {
		// we waited for another request, recheck the storage!
		process, ok = GetProcessFromStorage(pid)
		if ok {
			return process, nil
		}
		// if cache is still empty, go ahead
	} else {
		// we are the first!
		defer markRequestFinished()
	}

	// create new process
	new := &Process{
		Pid:       pid,
		Virtual:   true, // caller must decide to actually use the process - we need to save now.
		FirstSeen: time.Now().Unix(),
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
				return nil, fmt.Errorf("failed to get UID for p%d: %s", pid, err)
			}
			new.UserID = int(uids[0])
		}

		// Username
		new.UserName, err = pInfo.Username()
		if err != nil {
			return nil, fmt.Errorf("process: failed to get Username for p%d: %s", pid, err)
		}

		// TODO: User Home
		// new.UserHome, err =

		// PPID
		ppid, err := pInfo.Ppid()
		if err != nil {
			return nil, fmt.Errorf("failed to get PPID for p%d: %s", pid, err)
		}
		new.ParentPid = int(ppid)

		// Path
		new.Path, err = pInfo.Exe()
		if err != nil {
			return nil, fmt.Errorf("failed to get Path for p%d: %s", pid, err)
		}
		// remove linux " (deleted)" suffix for deleted files
		if onLinux {
			new.Path = strings.TrimSuffix(new.Path, " (deleted)")
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
			return nil, fmt.Errorf("failed to get Cmdline for p%d: %s", pid, err)
		}

		// Name
		new.Name, err = pInfo.Name()
		if err != nil {
			return nil, fmt.Errorf("failed to get Name for p%d: %s", pid, err)
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

func saveFailedProcess(pid int, err string) {
	failed := &Process{
		Pid:       pid,
		FirstSeen: time.Now().Unix(),
		Virtual:   true, // not needed
		Error:     err,
	}

	failed.Save()
}
