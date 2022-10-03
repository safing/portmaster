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

const onLinux = runtime.GOOS == "linux"

var getProcessSingleInflight singleflight.Group

// A Process represents a process running on the operating system.
type Process struct {
	record.Base
	sync.Mutex

	// Process attributes.
	// Don't change; safe for concurrent access.

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
	Env       map[string]string

	// Profile attributes.
	// Once set, these don't change; safe for concurrent access.

	// Tags holds extended information about the (virtual) process, which is used
	// to find a profile.
	Tags []profile.Tag
	// MatchingPath holds an alternative binary path that can be used to find a
	// profile.
	MatchingPath string

	// PrimaryProfileID holds the scoped ID of the primary profile.
	PrimaryProfileID string
	// profile holds the layered profile based on the primary profile.
	profile *profile.LayeredProfile

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

// IsIdentified returns whether the process has been identified or if it
// represents some kind of unidentified process.
func (p *Process) IsIdentified() bool {
	// Check if process exists.
	if p == nil {
		return false
	}

	// Check for special PIDs.
	switch p.Pid {
	case UndefinedProcessID:
		return false
	case UnidentifiedProcessID:
		return false
	case UnsolicitedProcessID:
		return false
	default:
		return true
	}
}

// Equal returns if the two processes are both identified and have the same PID.
func (p *Process) Equal(other *Process) bool {
	return p.IsIdentified() && other.IsIdentified() && p.Pid == other.Pid
}

const systemResolverScopedID = string(profile.SourceLocal) + "/" + profile.SystemResolverProfileID

// IsSystemResolver is a shortcut to check if the process is or belongs to the
// system resolver and needs special handling.
func (p *Process) IsSystemResolver() bool {
	// Check if process exists.
	if p == nil {
		return false
	}

	// Check ID.
	return p.PrimaryProfileID == systemResolverScopedID
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

// String returns a string representation of process.
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

	return p.(*Process), nil // nolint:forcetypeassert // Can only be a *Process.
}

func loadProcess(ctx context.Context, pid int) (*Process, error) {
	switch pid {
	case UnidentifiedProcessID:
		return GetUnidentifiedProcess(ctx), nil
	case UnsolicitedProcessID:
		return GetUnsolicitedProcess(ctx), nil
	case SystemProcessID:
		return GetSystemProcess(ctx), nil
	}

	process, ok := GetProcessFromStorage(pid)
	if ok {
		return process, nil
	}

	// Create new a process object.
	process = &Process{
		Pid:       pid,
		FirstSeen: time.Now().Unix(),
	}

	// Get process information from the system.
	pInfo, err := processInfo.NewProcessWithContext(ctx, int32(pid))
	if err != nil {
		return nil, err
	}

	// UID
	// net yet implemented for windows
	if onLinux {
		var uids []int32
		uids, err = pInfo.UidsWithContext(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get UID for p%d: %w", pid, err)
		}
		process.UserID = int(uids[0])
	}

	// Username
	process.UserName, err = pInfo.UsernameWithContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("process: failed to get Username for p%d: %w", pid, err)
	}

	// TODO: User Home
	// new.UserHome, err =

	// PPID
	ppid, err := pInfo.PpidWithContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get PPID for p%d: %w", pid, err)
	}
	process.ParentPid = int(ppid)

	// Path
	process.Path, err = pInfo.ExeWithContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get Path for p%d: %w", pid, err)
	}
	// remove linux " (deleted)" suffix for deleted files
	if onLinux {
		process.Path = strings.TrimSuffix(process.Path, " (deleted)")
	}
	// Executable Name
	_, process.ExecName = filepath.Split(process.Path)

	// Current working directory
	// net yet implemented for windows
	// new.Cwd, err = pInfo.Cwd()
	// if err != nil {
	// 	log.Warningf("process: failed to get Cwd: %w", err)
	// }

	// Command line arguments
	process.CmdLine, err = pInfo.CmdlineWithContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get Cmdline for p%d: %w", pid, err)
	}

	// Name
	process.Name, err = pInfo.NameWithContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get Name for p%d: %w", pid, err)
	}
	if process.Name == "" {
		process.Name = process.ExecName
	}

	// Get all environment variables
	env, err := pInfo.EnvironWithContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get the environment for p%d: %w", pid, err)
	}
	// Split env variables in key and value.
	process.Env = make(map[string]string, len(env))
	for _, entry := range env {
		splitted := strings.SplitN(entry, "=", 2)
		if len(splitted) == 2 {
			process.Env[strings.Trim(splitted[0], `'"`)] = strings.Trim(splitted[1], `'"`)
		}
	}

	// Add process tags.
	process.addTags()
	if len(process.Tags) > 0 {
		log.Tracer(ctx).Debugf("profile: added tags: %+v", process.Tags)
	}

	process.Save()
	return process, nil
}

// MatchingData returns the matching data for the process.
func (p *Process) MatchingData() *MatchingData {
	return &MatchingData{p}
}

// MatchingData provides a interface compatible view on the process for profile matching.
type MatchingData struct {
	p *Process
}

// Tags returns process.Tags.
func (md *MatchingData) Tags() []profile.Tag { return md.p.Tags }

// Env returns process.Env.
func (md *MatchingData) Env() map[string]string { return md.p.Env }

// Path returns process.Path.
func (md *MatchingData) Path() string { return md.p.Path }

// MatchingPath returns process.MatchingPath.
func (md *MatchingData) MatchingPath() string { return md.p.MatchingPath }
