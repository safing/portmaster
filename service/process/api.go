package process

import (
	"errors"
	"maps"
	"net/http"
	"slices"
	"strconv"

	"github.com/safing/portmaster/base/api"
	"github.com/safing/portmaster/service/profile"
)

func registerAPIEndpoints() error {
	if err := api.RegisterEndpoint(api.Endpoint{
		Name:        "Get Process Tag Metadata",
		Description: "Get information about process tags.",
		Path:        "process/tags",
		Read:        api.PermitUser,
		StructFunc:  handleProcessTagMetadata,
	}); err != nil {
		return err
	}

	if err := api.RegisterEndpoint(api.Endpoint{
		Name:        "Get Processes by Profile",
		Description: "Get all recently active processes using the given profile",
		Path:        "process/list/by-profile/{source:[a-z]+}/{id:[A-z0-9-]+}",
		Read:        api.PermitUser,
		StructFunc:  handleGetProcessesByProfile,
	}); err != nil {
		return err
	}

	if err := api.RegisterEndpoint(api.Endpoint{
		Name:        "Get Process Group Leader By PID",
		Description: "Load a process group leader by a child PID",
		Path:        "process/group-leader/{pid:[0-9]+}",
		Read:        api.PermitUser,
		StructFunc:  handleGetProcessGroupLeader,
	}); err != nil {
		return err
	}

	return nil
}

func handleProcessTagMetadata(ar *api.Request) (i interface{}, err error) {
	tagRegistryLock.Lock()
	defer tagRegistryLock.Unlock()

	// Create response struct.
	resp := struct {
		Tags []TagDescription
	}{
		Tags: make([]TagDescription, 0, len(tagRegistry)*2),
	}

	// Get all tag descriptions.
	for _, th := range tagRegistry {
		resp.Tags = append(resp.Tags, th.TagDescriptions()...)
	}

	return resp, nil
}

func handleGetProcessesByProfile(ar *api.Request) (any, error) {
	source := ar.URLVars["source"]
	id := ar.URLVars["id"]
	if id == "" || source == "" {
		return nil, api.ErrorWithStatus(errors.New("missing profile source/id"), http.StatusBadRequest)
	}

	result := GetProcessesWithProfile(ar.Context(), profile.ProfileSource(source), id, true)

	// Return snapshots, as the result is encoded after returning, without
	// holding the process locks.
	snapshots := make([]*Process, 0, len(result))
	for _, p := range result {
		snapshots = append(snapshots, p.snapshotForAPI())
	}
	return snapshots, nil
}

func handleGetProcessGroupLeader(ar *api.Request) (any, error) {
	pid, err := strconv.ParseInt(ar.URLVars["pid"], 10, 0)
	if err != nil {
		return nil, api.ErrorWithStatus(err, http.StatusBadRequest)
	}

	process, err := GetOrFindProcess(ar.Context(), int(pid))
	if err != nil {
		return nil, api.ErrorWithStatus(err, http.StatusInternalServerError)
	}
	err = process.FindProcessGroupLeader(ar.Context())
	leader := process.Leader()
	switch {
	case leader != nil:
		// Return a snapshot, as the result is encoded after returning, without
		// holding the process lock.
		return leader.snapshotForAPI(), nil
	case err != nil:
		return nil, api.ErrorWithStatus(err, http.StatusInternalServerError)
	default:
		return nil, api.ErrorWithStatus(errors.New("leader not found"), http.StatusNotFound)
	}
}

// snapshotForAPI returns a copy of the exported (serialized) attributes of
// the process, made while holding the process lock. Some of these attributes
// (eg. LastSeen, PrimaryProfileID) are changed under the process lock while
// the process is in use, so the live process must not be encoded by the API
// without holding its lock.
// The copy is only meant for encoding: it has no database key, metadata or
// profile and is not stored anywhere.
// Only the lock of this process is taken, no other locks are acquired while
// holding it.
func (p *Process) snapshotForAPI() *Process {
	p.Lock()
	defer p.Unlock()

	return &Process{
		Name:     p.Name,
		UserID:   p.UserID,
		UserName: p.UserName,
		UserHome: p.UserHome,

		Pid:       p.Pid,
		CreatedAt: p.CreatedAt,

		ParentPid:       p.ParentPid,
		ParentCreatedAt: p.ParentCreatedAt,

		LeaderPid: p.LeaderPid,

		Path:     p.Path,
		ExecName: p.ExecName,
		Cwd:      p.Cwd,
		CmdLine:  p.CmdLine,
		FirstArg: p.FirstArg,
		Env:      maps.Clone(p.Env),

		processKey: p.processKey,

		Tags:         slices.Clone(p.Tags),
		MatchingPath: p.MatchingPath,

		PrimaryProfileID: p.PrimaryProfileID,

		FirstSeen: p.FirstSeen,
		LastSeen:  p.LastSeen,
		Error:     p.Error,

		ExecHashes: maps.Clone(p.ExecHashes),
	}
}
