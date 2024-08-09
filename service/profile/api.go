package profile

import (
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/safing/portmaster/base/api"
	"github.com/safing/portmaster/base/utils"
	"github.com/safing/portmaster/service/profile/binmeta"
	"github.com/safing/structures/dsd"
)

func registerAPIEndpoints() error {
	if err := api.RegisterEndpoint(api.Endpoint{
		Name:        "Merge profiles",
		Description: "Merge multiple profiles into a new one.",
		Path:        "profile/merge",
		Write:       api.PermitUser,
		StructFunc:  handleMergeProfiles,
	}); err != nil {
		return err
	}

	if err := api.RegisterEndpoint(api.Endpoint{
		Name:        "Get Profile Icon",
		Description: "Returns the requested profile icon.",
		Path:        "profile/icon/{id:[a-f0-9]*\\.[a-z]{3,4}}",
		Read:        api.PermitUser,
		DataFunc:    handleGetProfileIcon,
	}); err != nil {
		return err
	}

	if err := api.RegisterEndpoint(api.Endpoint{
		Name:        "Update Profile Icon",
		Description: "Updates a profile icon.",
		Path:        "profile/icon",
		Write:       api.PermitUser,
		StructFunc:  handleUpdateProfileIcon,
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

func handleGetProfileIcon(ar *api.Request) (data []byte, err error) {
	name := ar.URLVars["id"]

	ext := filepath.Ext(name)

	// Get profile icon.
	data, err = binmeta.GetProfileIcon(name)
	switch {
	case err == nil:
		// Continue
	case errors.Is(err, binmeta.ErrIconIgnored):
		return nil, api.ErrorWithStatus(err, http.StatusNotFound)
	default:
		return nil, err
	}

	// Set content type for icon.
	contentType, ok := utils.MimeTypeByExtension(ext)
	if ok {
		ar.ResponseHeader.Set("Content-Type", contentType)
	}

	return data, nil
}

type updateProfileIconResponse struct {
	Filename string `json:"filename"`
}

//nolint:goconst
func handleUpdateProfileIcon(ar *api.Request) (any, error) {
	// Check input.
	if len(ar.InputData) == 0 {
		return nil, api.ErrorWithStatus(errors.New("no content"), http.StatusBadRequest)
	}
	mimeType := ar.Header.Get("Content-Type")
	if mimeType == "" {
		return nil, api.ErrorWithStatus(errors.New("no content type"), http.StatusBadRequest)
	}

	// Derive image format from content type.
	mimeType = strings.TrimSpace(mimeType)
	mimeType = strings.ToLower(mimeType)
	mimeType, _, _ = strings.Cut(mimeType, ";")
	var ext string
	switch mimeType {
	case "image/gif":
		ext = "gif"
	case "image/jpeg":
		ext = "jpg"
	case "image/jpg":
		ext = "jpg"
	case "image/png":
		ext = "png"
	case "image/svg+xml":
		ext = "svg"
	case "image/tiff":
		ext = "tiff"
	case "image/webp":
		ext = "webp"
	default:
		return "", api.ErrorWithStatus(errors.New("unsupported image format"), http.StatusBadRequest)
	}

	// Update profile icon.
	filename, err := binmeta.UpdateProfileIcon(ar.InputData, ext)
	if err != nil {
		return nil, err
	}

	return &updateProfileIconResponse{
		Filename: filename,
	}, nil
}
