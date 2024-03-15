package base

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/safing/portbase/dataroot"
	"github.com/safing/portbase/log"
	"github.com/safing/portbase/modules"
)

const (
	logTTL        = 30 * 24 * time.Hour
	logFileDir    = "logs"
	logFileSuffix = ".log"
)

func registerLogCleaner() {
	module.NewTask("log cleaner", logCleaner).
		Repeat(24 * time.Hour).
		Schedule(time.Now().Add(15 * time.Minute))
}

func logCleaner(_ context.Context, _ *modules.Task) error {
	ageThreshold := time.Now().Add(-logTTL)

	return filepath.Walk(
		filepath.Join(dataroot.Root().Path, logFileDir),
		func(path string, info os.FileInfo, err error) error {
			if err != nil {
				if !errors.Is(err, os.ErrNotExist) {
					log.Warningf("core: failed to access %s while deleting old log files: %s", path, err)
				}
				return nil
			}

			switch {
			case !info.Mode().IsRegular():
				// Only delete regular files.
			case !strings.HasSuffix(path, logFileSuffix):
				// Only delete files that end with the correct suffix.
			case info.ModTime().After(ageThreshold):
				// Only delete files that are older that the log TTL.
			default:
				// Delete log file.
				err := os.Remove(path)
				if err != nil {
					log.Warningf("core: failed to delete old log file %s: %s", path, err)
				} else {
					log.Tracef("core: deleted old log file %s", path)
				}
			}

			return nil
		})
}
