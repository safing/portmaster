package captain

import (
	"errors"
	"fmt"

	"github.com/safing/portbase/api"
	"github.com/safing/portbase/database"
	"github.com/safing/portbase/database/query"
	"github.com/safing/portbase/modules"
)

const (
	apiPathForSPNReInit = "spn/reinit"
)

func registerAPIEndpoints() error {
	if err := api.RegisterEndpoint(api.Endpoint{
		Path:  apiPathForSPNReInit,
		Write: api.PermitAdmin,
		// BelongsTo:   module, // Do not attach to module, as this must run outside of the module.
		ActionFunc:  handleReInit,
		Name:        "Re-initialize SPN",
		Description: "Stops the SPN, resets all caches and starts it again. The SPN account and settings are not changed.",
	}); err != nil {
		return err
	}

	return nil
}

func handleReInit(ar *api.Request) (msg string, err error) {
	// Disable module and check
	changed := module.Disable()
	if !changed {
		return "", errors.New("can only re-initialize when the SPN is enabled")
	}

	// Run module manager.
	err = modules.ManageModules()
	if err != nil {
		return "", fmt.Errorf("failed to stop SPN: %w", err)
	}

	// Delete SPN cache.
	db := database.NewInterface(&database.Options{
		Local:    true,
		Internal: true,
	})
	deletedRecords, err := db.Purge(ar.Context(), query.New("cache:spn/"))
	if err != nil {
		return "", fmt.Errorf("failed to delete SPN cache: %w", err)
	}

	// Enable module.
	module.Enable()

	// Run module manager.
	err = modules.ManageModules()
	if err != nil {
		return "", fmt.Errorf("failed to start SPN after cache reset: %w", err)
	}

	return fmt.Sprintf(
		"Completed SPN re-initialization and deleted %d cache records in the process.",
		deletedRecords,
	), nil
}
