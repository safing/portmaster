package process

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/safing/portbase/api"
)

func registerAPIEndpoints() error {
	if err := api.RegisterEndpoint(api.Endpoint{
		Path:        "process/tags",
		Read:        api.PermitUser,
		BelongsTo:   module,
		StructFunc:  handleProcessTagMetadata,
		Name:        "Get Process Tag Metadata",
		Description: "Get information about process tags.",
	}); err != nil {
		return err
	}

	if err := api.RegisterEndpoint(api.Endpoint{
		Path: "process/by-profile",
		Parameters: []api.Parameter{
			{
				Method:      http.MethodGet,
				Field:       "scopedId",
				Value:       "",
				Description: "The ID of the profile",
			},
		},
		Read:      api.PermitUser,
		BelongsTo: module,
		StructFunc: api.StructFunc(func(ar *api.Request) (any, error) {
			id := ar.URL.Query().Get("scopedId")

			if id == "" {
				return nil, api.ErrorWithStatus(fmt.Errorf("missing profile id"), http.StatusBadRequest)
			}

			result := FindProcessesByProfile(ar.Context(), id)

			return result, nil
		}),
		Description: "Get all running processes for a given profile",
		Name:        "Get Processes by Profile",
	}); err != nil {
		return err
	}

	if err := api.RegisterEndpoint(api.Endpoint{
		Path: "process/by-pid/{pid:[0-9]+}",
		Parameters: []api.Parameter{
			{
				Method:      http.MethodGet,
				Field:       "pid",
				Value:       "",
				Description: "A PID of a process inside the requested process group",
			},
		},
		Read:      api.PermitUser,
		BelongsTo: module,
		StructFunc: api.StructFunc(func(ar *api.Request) (i interface{}, err error) {
			pid, err := strconv.ParseInt(ar.URLVars["pid"], 10, 0)
			if err != nil {
				return nil, api.ErrorWithStatus(err, http.StatusBadRequest)
			}

			process, err := GetProcessGroupLeader(ar.Context(), int(pid))
			if err != nil {
				return nil, api.ErrorWithStatus(err, http.StatusInternalServerError)
			}

			return process, nil
		}),
		Description: "Load a process group leader by a child PID",
		Name:        "Get Process Group Leader By PID",
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
