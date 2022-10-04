package process

import (
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
