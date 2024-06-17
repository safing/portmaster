package profile

import (
	"errors"
	"fmt"
	"os"
	"sync/atomic"

	"github.com/safing/portmaster/base/database"
	"github.com/safing/portmaster/base/database/migration"
	"github.com/safing/portmaster/base/dataroot"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/modules"
	_ "github.com/safing/portmaster/service/core/base"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/profile/binmeta"
	"github.com/safing/portmaster/service/updates"
)

var (
	migrations = migration.New("core:migrations/profile")
	// module      *modules.Module
	updatesPath string
)

// Events.
const (
	ConfigChangeEvent = "profile config change"
	DeletedEvent      = "profile deleted"
	MigratedEvent     = "profile migrated"
)

type ProfileModule struct {
	mgr      *mgr.Manager
	instance instance

	EventConfigChange *mgr.EventMgr[string]
	EventDelete       *mgr.EventMgr[string]
	EventMigrated     *mgr.EventMgr[[]string]
}

func (pm *ProfileModule) Start(m *mgr.Manager) error {
	pm.mgr = m

	pm.EventConfigChange = mgr.NewEventMgr[string](ConfigChangeEvent, m)
	pm.EventDelete = mgr.NewEventMgr[string](DeletedEvent, m)
	pm.EventMigrated = mgr.NewEventMgr[[]string](MigratedEvent, m)

	if err := prep(); err != nil {
		return err
	}

	return start()
}

func (pm *ProfileModule) Stop(m *mgr.Manager) error {
	return stop()
}

func prep() error {
	if err := registerConfiguration(); err != nil {
		return err
	}

	if err := registerConfigUpdater(); err != nil {
		return err
	}

	if err := registerMigrations(); err != nil {
		return err
	}

	// Setup icon storage location.
	iconsDir := dataroot.Root().ChildDir("databases", 0o0700).ChildDir("icons", 0o0700)
	if err := iconsDir.Ensure(); err != nil {
		return fmt.Errorf("failed to create/check icons directory: %w", err)
	}
	binmeta.ProfileIconStoragePath = iconsDir.Path

	return nil
}

func start() error {
	updatesPath = updates.RootPath()
	if updatesPath != "" {
		updatesPath += string(os.PathSeparator)
	}

	if err := loadProfilesMetadata(); err != nil {
		if !errors.Is(err, database.ErrNotFound) {
			log.Warningf("profile: failed to load profiles metadata, falling back to empty state: %s", err)
		}
		meta = &ProfilesMetadata{}
	}
	meta.check()

	if err := migrations.Migrate(module.mgr.Ctx()); err != nil {
		log.Errorf("profile: migrations failed: %s", err)
	}

	err := registerValidationDBHook()
	if err != nil {
		return err
	}

	err = registerRevisionProvider()
	if err != nil {
		return err
	}

	err = startProfileUpdateChecker()
	if err != nil {
		return err
	}

	module.mgr.Go("clean active profiles", cleanActiveProfiles)

	err = updateGlobalConfigProfile(module.mgr.Ctx(), nil)
	if err != nil {
		log.Warningf("profile: error during loading global profile from configuration: %s", err)
	}

	if err := registerAPIEndpoints(); err != nil {
		return err
	}

	return nil
}

func stop() error {
	return meta.Save()
}

var (
	module     *ProfileModule
	shimLoaded atomic.Bool
)

func NewModule(instance instance) (*ProfileModule, error) {
	if !shimLoaded.CompareAndSwap(false, true) {
		return nil, errors.New("only one instance allowed")
	}

	module = &ProfileModule{
		instance: instance,
	}

	return module, nil
}

type instance interface{}
