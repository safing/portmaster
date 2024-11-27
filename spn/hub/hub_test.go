package hub

import (
	"fmt"
	"net"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/safing/portmaster/base/api"
	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/base/database/dbmodule"
	"github.com/safing/portmaster/base/notifications"
	"github.com/safing/portmaster/service/core/base"
	"github.com/safing/portmaster/service/updates"
)

type testInstance struct {
	db      *dbmodule.DBModule
	api     *api.API
	config  *config.Config
	updates *updates.Updater
	base    *base.Base
}

func (stub *testInstance) IntelUpdates() *updates.Updater {
	return stub.updates
}

func (stub *testInstance) API() *api.API {
	return stub.api
}

func (stub *testInstance) Config() *config.Config {
	return stub.config
}

func (stub *testInstance) Notifications() *notifications.Notifications {
	return nil
}

func (stub *testInstance) Base() *base.Base {
	return stub.base
}

func (stub *testInstance) Ready() bool {
	return true
}

func (stub *testInstance) Restart() {}

func (stub *testInstance) Shutdown() {}

func (stub *testInstance) SetCmdLineOperation(f func() error) {}

func runTest(m *testing.M) error {
	api.SetDefaultAPIListenAddress("0.0.0.0:8080")
	ds, err := config.InitializeUnitTestDataroot("test-hub")
	if err != nil {
		return fmt.Errorf("failed to initialize dataroot: %w", err)
	}
	defer func() { _ = os.RemoveAll(ds) }()

	installDir, err := os.MkdirTemp("", "hub_installdir")
	if err != nil {
		return fmt.Errorf("failed to create tmp install dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(installDir) }()
	err = updates.GenerateMockFolder(installDir, "Test Intel", "1.0.0")
	if err != nil {
		return fmt.Errorf("failed to generate mock installation: %w", err)
	}

	stub := &testInstance{}
	// Init
	stub.db, err = dbmodule.New(stub)
	if err != nil {
		return fmt.Errorf("failed to create database: %w", err)
	}
	stub.api, err = api.New(stub)
	if err != nil {
		return fmt.Errorf("failed to create api: %w", err)
	}
	stub.config, err = config.New(stub)
	if err != nil {
		return fmt.Errorf("failed to create config: %w", err)
	}
	stub.updates, err = updates.New(stub, "Test Intel", updates.Config{
		Directory: installDir,
		IndexFile: "index.json",
	})
	if err != nil {
		return fmt.Errorf("failed to create updates: %w", err)
	}
	stub.base, err = base.New(stub)
	if err != nil {
		return fmt.Errorf("failed to base updates: %w", err)
	}

	// Start
	err = stub.db.Start()
	if err != nil {
		return fmt.Errorf("failed to start database: %w", err)
	}
	err = stub.api.Start()
	if err != nil {
		return fmt.Errorf("failed to start api: %w", err)
	}
	err = stub.config.Start()
	if err != nil {
		return fmt.Errorf("failed to start config: %w", err)
	}
	err = stub.updates.Start()
	if err != nil {
		return fmt.Errorf("failed to start updates: %w", err)
	}
	err = stub.base.Start()
	if err != nil {
		return fmt.Errorf("failed to start base: %w", err)
	}

	m.Run()
	return nil
}

func TestMain(m *testing.M) {
	if err := runTest(m); err != nil {
		fmt.Printf("%s", err)
		os.Exit(1)
	}
}

func TestEquality(t *testing.T) {
	t.Parallel()

	// empty match
	a := &Announcement{}
	assert.True(t, a.Equal(a), "should match itself") //nolint:gocritic // This is a test.

	// full match
	a = &Announcement{
		ID:             "a",
		Timestamp:      1,
		Name:           "a",
		ContactAddress: "a",
		ContactService: "a",
		Hosters:        []string{"a", "b"},
		Datacenter:     "a",
		IPv4:           net.IPv4(1, 2, 3, 4),
		IPv6:           net.ParseIP("::1"),
		Transports:     []string{"a", "b"},
		Entry:          []string{"a", "b"},
		Exit:           []string{"a", "b"},
	}
	assert.True(t, a.Equal(a), "should match itself") //nolint:gocritic // This is a test.

	// no match
	b := &Announcement{ID: "b"}
	assert.False(t, a.Equal(b), "should not match")
	b = &Announcement{Timestamp: 2}
	assert.False(t, a.Equal(b), "should not match")
	b = &Announcement{Name: "b"}
	assert.False(t, a.Equal(b), "should not match")
	b = &Announcement{ContactAddress: "b"}
	assert.False(t, a.Equal(b), "should not match")
	b = &Announcement{ContactService: "b"}
	assert.False(t, a.Equal(b), "should not match")
	b = &Announcement{Hosters: []string{"b", "c"}}
	assert.False(t, a.Equal(b), "should not match")
	b = &Announcement{Datacenter: "b"}
	assert.False(t, a.Equal(b), "should not match")
	b = &Announcement{IPv4: net.IPv4(1, 2, 3, 5)}
	assert.False(t, a.Equal(b), "should not match")
	b = &Announcement{IPv6: net.ParseIP("::2")}
	assert.False(t, a.Equal(b), "should not match")
	b = &Announcement{Transports: []string{"b", "c"}}
	assert.False(t, a.Equal(b), "should not match")
	b = &Announcement{Entry: []string{"b", "c"}}
	assert.False(t, a.Equal(b), "should not match")
	b = &Announcement{Exit: []string{"b", "c"}}
	assert.False(t, a.Equal(b), "should not match")
}

func TestStringify(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "<Hub abcdefg>", (&Hub{ID: "abcdefg", Info: &Announcement{}}).String())
	assert.Equal(t, "<Hub abcd-efgh>", (&Hub{ID: "abcdefgh", Info: &Announcement{}}).String())
	assert.Equal(t, "<Hub bcde-fghi>", (&Hub{ID: "abcdefghi", Info: &Announcement{}}).String())
	assert.Equal(t, "<Hub Franz bcde-fghi>", (&Hub{ID: "abcdefghi", Info: &Announcement{Name: "Franz"}}).String())
	assert.Equal(t, "<Hub AProbablyAutoGen bcde-fghi>", (&Hub{ID: "abcdefghi", Info: &Announcement{Name: "AProbablyAutoGeneratedName"}}).String())
}
