package profile

import (
	"errors"
	"fmt"
	"os"
	"sync/atomic"

	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/base/database"
	"github.com/safing/portmaster/base/database/migration"
	"github.com/safing/portmaster/base/dataroot"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/utils"
	_ "github.com/safing/portmaster/service/core/base"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/profile/binmeta"
	"github.com/safing/portmaster/service/updates"
)

var (
	migrations  = migration.New("core:migrations/profile")
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

	states *mgr.StateMgr
}

func (pm *ProfileModule) Manager() *mgr.Manager {
	return pm.mgr
}

func (pm *ProfileModule) States() *mgr.StateMgr {
	return pm.states
}

func (pm *ProfileModule) Start() error {
	return start()
}

func (pm *ProfileModule) Stop() error {
	return stop()
}

func prep() error {
	if err := registerConfiguration(); err != nil {
		return err
	}

	if err := registerMigrations(); err != nil {
		return err
	}

	if err := registerAPIEndpoints(); err != nil {
		return err
	}

	// Setup icon storage location.
	iconsDir := dataroot.Root().ChildDir("databases", utils.AdminOnlyPermission).ChildDir("icons", utils.AdminOnlyPermission)
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

	// Register config callback when starting, as it depends on the updates module,
	// but the config system will already submit events earlier.
	if err := registerGlobalConfigProfileUpdater(); err != nil {
		return err
	}

	err = updateGlobalConfigProfile(module.mgr.Ctx())
	if err != nil {
		log.Warningf("profile: error during loading global profile from configuration: %s", err)
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
	m := mgr.New("Profile")
	module = &ProfileModule{
		mgr:      m,
		instance: instance,

		EventConfigChange: mgr.NewEventMgr[string](ConfigChangeEvent, m),
		EventDelete:       mgr.NewEventMgr[string](DeletedEvent, m),
		EventMigrated:     mgr.NewEventMgr[[]string](MigratedEvent, m),

		states: mgr.NewStateMgr(m),
	}

	if err := prep(); err != nil {
		return nil, err
	}

	return module, nil
}

type instance interface {
	Config() *config.Config
}
