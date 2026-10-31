package process

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/safing/portmaster/base/api"
	"github.com/safing/portmaster/service/profile"
)

// registerAPIEndpoints registers the process API endpoints.
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
		Parameters: []api.Parameter{{
			Method:      http.MethodGet,
			Field:       "leaders",
			Value:       "false",
			Description: "Return the processes that match the profile themselves. By default (leaders omitted or any other value) each process is replaced by its process group leader when one is available.",
		}},
		StructFunc: handleGetProcessesByProfile,
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

// handleProcessTagMetadata returns the descriptions of all registered process tags.
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

// handleGetProcessesByProfile returns the recently active processes of a profile; by default each
// is replaced by its process group leader when one is available, with leaders=false the matching
// processes themselves are returned.
func handleGetProcessesByProfile(ar *api.Request) (any, error) {
	source := ar.URLVars["source"]
	id := ar.URLVars["id"]
	if id == "" || source == "" {
		return nil, api.ErrorWithStatus(errors.New("missing profile source/id"), http.StatusBadRequest)
	}

	// By default the process group leader is returned for each matching
	// process, as it usually carries the better name and icon information.
	// Callers that need the owning processes themselves (e.g. to resolve an
	// icon for a flatpak, whose leader is the shared bwrap launcher) pass
	// leaders=false.
	preferLeaders := ar.URL.Query().Get("leaders") != "false"
	result := GetProcessesWithProfile(ar.Context(), profile.ProfileSource(source), id, preferLeaders)
	return result, nil
}

// handleGetProcessGroupLeader returns the process group leader of the process with the given PID.
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
	switch {
	case process.Leader() != nil:
		return process.Leader(), nil
	case err != nil:
		return nil, api.ErrorWithStatus(err, http.StatusInternalServerError)
	default:
		return nil, api.ErrorWithStatus(errors.New("leader not found"), http.StatusNotFound)
	}
}
