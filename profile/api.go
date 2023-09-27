package profile

import (
	"fmt"

	"github.com/safing/portbase/api"
	"github.com/safing/portbase/formats/dsd"
)

func registerAPIEndpoints() error {
	if err := api.RegisterEndpoint(api.Endpoint{
		Name:        "Merge profiles",
		Description: "Merge multiple profiles into a new one.",
		Path:        "profile/merge",
		Write:       api.PermitUser,
		BelongsTo:   module,
		StructFunc:  handleMergeProfiles,
	}); err != nil {
		return err
	}

	return nil
}

type mergeProfilesRequest struct {
	Name string   `json:"name"` // Name of the new merged profile.
	To   string   `json:"to"`   // Profile scoped ID.
	From []string `json:"from"` // Profile scoped IDs.
}

type mergeprofilesResponse struct {
	New string `json:"new"` // Profile scoped ID.
}

func handleMergeProfiles(ar *api.Request) (i interface{}, err error) {
	request := &mergeProfilesRequest{}
	_, err = dsd.MimeLoad(ar.InputData, ar.Header.Get("Content-Type"), request)
	if err != nil {
		return nil, fmt.Errorf("failed to parse request: %w", err)
	}

	// Get all profiles.
	var (
		primary     *Profile
		secondaries = make([]*Profile, 0, len(request.From))
	)
	if primary, err = getProfile(request.To); err != nil {
		return nil, fmt.Errorf("failed to get profile %s: %w", request.To, err)
	}
	for _, from := range request.From {
		sp, err := getProfile(from)
		if err != nil {
			return nil, fmt.Errorf("failed to get profile %s: %w", request.To, err)
		}
		secondaries = append(secondaries, sp)
	}

	newProfile, err := MergeProfiles(request.Name, primary, secondaries...)
	if err != nil {
		return nil, fmt.Errorf("failed to merge profiles: %w", err)
	}

	return &mergeprofilesResponse{
		New: newProfile.ScopedID(),
	}, nil
}
