package config

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestConcurrentSaveConfig checks that concurrent saves (as triggered by
// concurrent SetConfigOption calls) do not deadlock.
func TestConcurrentSaveConfig(t *testing.T) { //nolint:paralleltest // Uses global state.
	options = make(map[string]*Option)
	for i := range 50 {
		quickRegister(t, fmt.Sprintf("concurrent/opt%d", i), OptTypeString, "default")
	}

	oldPath := configFilePath
	configFilePath = filepath.Join(t.TempDir(), "config.json")
	defer func() { configFilePath = oldPath }()

	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			for j := range 20 {
				err := setConfigOption(fmt.Sprintf("concurrent/opt%d", (i*7+j)%50), fmt.Sprintf("v%d-%d", i, j), false)
				if err != nil {
					t.Error(err)
					return
				}
			}
		})
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("concurrent config saves deadlocked")
	}
}
