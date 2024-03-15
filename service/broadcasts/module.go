package broadcasts

import (
	"sync"
	"time"

	"github.com/safing/portbase/database"
	"github.com/safing/portbase/modules"
)

var (
	module *modules.Module

	db = database.NewInterface(&database.Options{
		Local:    true,
		Internal: true,
	})

	startOnce sync.Once
)

func init() {
	module = modules.Register("broadcasts", prep, start, nil, "updates", "netenv", "notifications")
}

func prep() error {
	// Register API endpoints.
	if err := registerAPIEndpoints(); err != nil {
		return err
	}

	return nil
}

func start() error {
	// Ensure the install info is up to date.
	ensureInstallInfo()

	// Start broadcast notifier task.
	startOnce.Do(func() {
		module.NewTask("broadcast notifier", broadcastNotify).
			Repeat(10 * time.Minute).Queue()
	})

	return nil
}
