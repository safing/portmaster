package process

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	processInfo "github.com/shirou/gopsutil/process"
	"golang.org/x/sync/singleflight"

	"github.com/safing/portmaster/base/database/record"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/profile"
)

const onLinux = runtime.GOOS == "linux"

var getProcessSingleInflight singleflight.Group

// A Process represents a process running on the operating system.
type Process struct {
	record.Base
	sync.Mutex

	// Process attributes.
	// Don't change; safe for concurrent access.

	Name     string
	UserID   int
	UserName string
	UserHome string

	Pid       int
	CreatedAt int64

	ParentPid       int
	ParentCreatedAt int64

	LeaderPid int
	leader    *Process

	Path     string
	ExecName string
	Cwd      string
	CmdLine  string
	FirstArg string
	Env      map[string]string

	// unique process identifier ("Pid-CreatedAt")
	processKey string

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

// GetTag returns the process tag with the given ID.
func (p *Process) GetTag(tagID string) (profile.Tag, bool) {
	for _, t := range p.Tags {
		if t.Key == tagID {
			return t, true
		}
	}
	return profile.Tag{}, false
}

// Profile returns the assigned layered profile.
func (p *Process) Profile() *profile.LayeredProfile {
	if p == nil {
		return nil
	}

	return p.profile
}

// Leader returns the process group leader that is attached to the process.
// This will not trigger a new search for the process group leader, it only
// returns existing data.
func (p *Process) Leader() *Process {
	p.Lock()
	defer p.Unlock()

	return p.leader
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

// HasValidPID returns whether the process has valid PID of an actual process.
func (p *Process) HasValidPID() bool {
	// Check if process exists.
	if p == nil {
		return false
	}

	return p.Pid >= 0
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

	// Check for special processes
	switch pid {
	case UnidentifiedProcessID:
		return GetUnidentifiedProcess(ctx), nil
	case UnsolicitedProcessID:
		return GetUnsolicitedProcess(ctx), nil
	case SystemProcessID:
		return GetSystemProcess(ctx), nil
	}

	// Get pid and creation time for identification.
	pInfo, err := processInfo.NewProcessWithContext(ctx, int32(pid))
	if err != nil {
		return nil, err
	}
	createdAt, err := pInfo.CreateTimeWithContext(ctx)
	if err != nil {
		return nil, err
	}
	key := getProcessKey(int32(pid), createdAt)

	// Load process and make sure it is only loaded once.
	p, err, _ := getProcessSingleInflight.Do(key, func() (interface{}, error) {
		return loadProcess(ctx, key, pInfo)
	})
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, errors.New("process getter returned nil")
	}

	return p.(*Process), nil // nolint:forcetypeassert // Can only be a *Process.
}

func loadProcess(ctx context.Context, key string, pInfo *processInfo.Process) (*Process, error) {
	// Check if we already have the process.
	process, ok := GetProcessFromStorage(key)
	if ok {
		return process, nil
	}

	// Create new a process object.
	process = &Process{
		Pid:        int(pInfo.Pid),
		FirstSeen:  time.Now().Unix(),
		processKey: key,
	}

	// Get creation time of process. (The value should be cached by the library.)
	var err error
	process.CreatedAt, err = pInfo.CreateTimeWithContext(ctx)
	if err != nil {
		return nil, err
	}

	// UID
	// TODO: implemented for windows
	if onLinux {
		var uids []int32
		uids, err = pInfo.UidsWithContext(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get UID for p%d: %w", pInfo.Pid, err)
		}
		process.UserID = int(uids[0])
	}

	// Username
	process.UserName, err = pInfo.UsernameWithContext(ctx)
	if err != nil {
		log.Tracer(ctx).Warningf("process: failed to get username (PID %d): %s", pInfo.Pid, err)
	}

	// TODO: User Home
	// new.UserHome, err =

	// Parent process ID
	ppid, err := pInfo.PpidWithContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get PPID for p%d: %w", pInfo.Pid, err)
	}
	process.ParentPid = int(ppid)

	// Parent created time
	parentPInfo, err := processInfo.NewProcessWithContext(ctx, ppid)
	if err == nil {
		parentCreatedAt, err := parentPInfo.CreateTimeWithContext(ctx)
		if err != nil {
			return nil, err
		}
		process.ParentCreatedAt = parentCreatedAt
	}

	// Leader process ID
	// Get process group ID to find group leader, which is the process "nearest"
	// to the user and will have more/better information for finding names and
	// icons, for example.
	leaderPid, err := GetProcessGroupID(ctx, process.Pid)
	if err != nil {
		// Fail gracefully.
		log.Warningf("process: failed to get process group ID for p%d: %s", process.Pid, err)
		process.LeaderPid = UndefinedProcessID
	} else {
		process.LeaderPid = leaderPid
	}

	// Path
	process.Path, err = pInfo.ExeWithContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get Path for p%d: %w", pInfo.Pid, err)
	}
	// remove linux " (deleted)" suffix for deleted files
	if onLinux {
		process.Path = strings.TrimSuffix(process.Path, " (deleted)")
	}
	// Executable Name
	_, process.ExecName = filepath.Split(process.Path)

	// Current working directory
	// not yet implemented for windows
	if runtime.GOOS != "windows" {
		process.Cwd, err = pInfo.CwdWithContext(ctx)
		if err != nil {
			log.Warningf("process: failed to get current working dir (PID %d): %s", pInfo.Pid, err)
		}
	}

	// Command line arguments
	process.CmdLine, err = pInfo.CmdlineWithContext(ctx)
	if err != nil {
		log.Tracer(ctx).Warningf("process: failed to get cmdline (PID %d): %s", pInfo.Pid, err)
	}

	// Name
	process.Name, err = pInfo.NameWithContext(ctx)
	if err != nil {
		log.Tracer(ctx).Warningf("process: failed to get process name (PID %d): %s", pInfo.Pid, err)
	}
	if process.Name == "" {
		process.Name = process.ExecName
	}

	// Get all environment variables
	env, err := pInfo.EnvironWithContext(ctx)
	if err == nil {
		// Split env variables in key and value.
		process.Env = make(map[string]string, len(env))
		for _, entry := range env {
			splitted := strings.SplitN(entry, "=", 2)
			if len(splitted) == 2 {
				process.Env[strings.Trim(splitted[0], `'"`)] = strings.Trim(splitted[1], `'"`)
			}
		}
	} else {
		log.Tracer(ctx).Warningf("process: failed to get the process environment (PID %d): %s", pInfo.Pid, err)
	}

	// Add process tags.
	process.addTags()
	if len(process.Tags) > 0 {
		log.Tracer(ctx).Debugf("profile: added tags: %+v", process.Tags)
	}

	process.Save()
	return process, nil
}

// GetKey returns the key that is used internally to identify the process.
// The key consists of the PID and the start time of the process as reported by
// the system.
func (p *Process) GetKey() string {
	return p.processKey
}

// Builds a unique identifier for a processes.
func getProcessKey(pid int32, createdTime int64) string {
	return fmt.Sprintf("%d-%d", pid, createdTime)
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

// Cmdline returns the command line of the process.
func (md *MatchingData) Cmdline() string { return md.p.CmdLine }
