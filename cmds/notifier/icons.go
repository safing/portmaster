package main

import (
	"os"
	"path/filepath"
	"sync"

	icons "github.com/safing/portmaster/assets"
)

var (
	appIconEnsureOnce sync.Once
	appIconPath       string
)

func ensureAppIcon() (location string, err error) {
	appIconEnsureOnce.Do(func() {
		if appIconPath == "" {
			appIconPath = filepath.Join(dataDir, "exec", "portmaster.png")
		}
		err = os.WriteFile(appIconPath, icons.PNG, 0o0644) // nolint:gosec
	})

	return appIconPath, err
}
