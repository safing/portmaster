package captain

import (
	"fmt"

	"github.com/safing/portmaster/base/api"
	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/base/database"
	"github.com/safing/portmaster/base/database/query"
	"github.com/safing/portmaster/spn/conf"
)

const (
	apiPathForSPNReInit = "spn/reinit"
)

func registerAPIEndpoints() error {
	if err := api.RegisterEndpoint(api.Endpoint{
		Path:        apiPathForSPNReInit,
		Write:       api.PermitAdmin,
		ActionFunc:  handleReInit,
		Name:        "Re-initialize SPN",
		Description: "Stops the SPN, resets all caches and starts it again. The SPN account and settings are not changed.",
	}); err != nil {
		return err
	}

	return nil
}

func handleReInit(ar *api.Request) (msg string, err error) {
	if !conf.Client() && !conf.Integrated() {
		return "", fmt.Errorf("re-initialization only possible on integrated clients")
	}

	// Make sure SPN is stopped and wait for it to complete.
	err = module.mgr.Do("stop SPN for re-init", module.instance.SPNGroup().EnsureStoppedWorker)
	if err != nil {
		return "", fmt.Errorf("failed to stop SPN for re-init: %w", err)
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

	// Start SPN if it is enabled.
	enabled := config.GetAsBool("spn/enable", false)
	if enabled() {
		module.mgr.Go("ensure SPN is started", module.instance.SPNGroup().EnsureStartedWorker)
	}

	return fmt.Sprintf(
		"Completed SPN re-initialization and deleted %d cache records in the process.",
		deletedRecords,
	), nil
}
