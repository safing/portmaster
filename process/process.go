package process

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	processInfo "github.com/shirou/gopsutil/process"
	"golang.org/x/sync/singleflight"

	"github.com/safing/portbase/database/record"
	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/profile"
)

const (
	onLinux = runtime.GOOS == "linux"
)

var getProcessSingleInflight singleflight.Group

// A Process represents a process running on the operating system.
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

	// SpecialDetail holds special information, the meaning of which can change
	// based on any of the previous attributes.
	SpecialDetail string

	LocalProfileKey string
	profile         *profile.LayeredProfile

	// Mutable attributes.

	FirstSeen int64
	LastSeen  int64
	Error     string // Cache errors

	ExecHashes map[string]string
}

// Profile returns the assigned layered profile.
func (p *Process) Profile() *profile.LayeredProfile {
	if p == nil {
		return nil
	}

	return p.profile
}

// IsSystemResolver is a shortcut to check if the process is or belongs to the
// system resolver and needs special handling.
func (p *Process) IsSystemResolver() bool {
	// Check if process exists.
	if p == nil {
		return false
	}

	// Check if local profile exists.
	localProfile := p.profile.LocalProfile()
	if localProfile == nil {
		return false
	}

	// Check ID.
	return localProfile.ID == profile.SystemResolverProfileID
}

// GetLastSeen returns the unix timestamp when the process was last seen.
func (p *Process) GetLastSeen() int64 {
	p.Lock()
	defer p.Unlock()

	return p.LastSeen
}

// SetLastSeen sets the unix timestamp when the process was last seen.
func (p *Process) SetLastSeen(lastSeen int64) {
	p.Lock()
	defer p.Unlock()

	p.LastSeen = lastSeen
}

// Strings returns a string representation of process.
func (p *Process) String() string {
	if p == nil {
		return "?"
	}

	return fmt.Sprintf("%s:%s:%d", p.UserName, p.Path, p.Pid)
}

// GetOrFindProcess returns the process for the given PID.
func GetOrFindProcess(ctx context.Context, pid int) (*Process, error) {
	log.Tracer(ctx).Tracef("process: getting process for PID %d", pid)

	p, err, _ := getProcessSingleInflight.Do(strconv.Itoa(pid), func() (interface{}, error) {
		return loadProcess(ctx, pid)
	})
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, errors.New("process getter returned nil")
	}

	return p.(*Process), nil
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

	// Create new a process object.
	new := &Process{
		Pid:       pid,
		FirstSeen: time.Now().Unix(),
	}

	// Get process information from the system.
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

	new.Save()
	return new, nil
}
