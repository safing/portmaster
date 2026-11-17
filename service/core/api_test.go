package core

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/safing/portmaster/base/api"
	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/base/notifications"
	"github.com/safing/portmaster/base/rng"
)

type configTestInstance struct {
	dataDir string
}

func (i *configTestInstance) DataDir() string                  { return i.dataDir }
func (i *configTestInstance) SetCmdLineOperation(func() error) {}

var appAuthorizationTestSetup sync.Once

func setupAppAuthorizationTest(t *testing.T) {
	t.Helper()

	appAuthorizationTestSetup.Do(func() {
		r, err := rng.New(struct{}{})
		if err != nil {
			t.Fatalf("failed to create rng module: %s", err)
		}
		err = r.Start()
		if err != nil {
			t.Fatalf("failed to start rng module: %s", err)
		}

		_, err = config.New(&configTestInstance{dataDir: t.TempDir()})
		if err != nil {
			t.Fatalf("failed to create config module: %s", err)
		}
		err = config.Register(&config.Option{
			Name:         "API Keys",
			Key:          api.CfgAPIKeys,
			Description:  "test option",
			OptType:      config.OptTypeStringArray,
			DefaultValue: []string{},
		})
		if err != nil {
			t.Fatalf("failed to register option: %s", err)
		}
	})
}

// TestAppAuthorizationAllowWithoutWaiter checks that the action function of an
// app authorization prompt does not block when the user allows access after
// the requesting API call has already returned (nobody reads the result).
// A blocked action function holds the notification lock forever, which also
// blocks the notification cleaner.
//
//nolint:paralleltest // Uses the global config.
func TestAppAuthorizationAllowWithoutWaiter(t *testing.T) {
	setupAppAuthorizationTest(t)

	ch, actionFn := newAppAuthorization("user", "user", time.Now().Add(time.Hour))
	n := &notifications.Notification{SelectedActionID: appAuthAllowActionID}

	done := make(chan error, 1)
	go func() {
		done <- actionFn(context.Background(), n)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("action function failed: %s", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("action function blocked because nobody waits for the result")
	}

	// The notification must not stay locked.
	n.Lock()
	n.Unlock() //nolint:staticcheck // Only checks that the lock is free.

	// A late reader still gets the key.
	if key := <-ch; key == "" {
		t.Fatal("expected API key on result channel")
	}
}
