package main

import (
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	processInfo "github.com/shirou/gopsutil/process"
)

func checkAndCreateInstanceLock(path, name string, perUser bool) (pid int32, err error) {
	lockFilePath := getLockFilePath(path, name, perUser)

	// read current pid file
	data, err := os.ReadFile(lockFilePath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			// create new lock
			return 0, createInstanceLock(lockFilePath)
		}
		return 0, err
	}

	// file exists!
	parsedPid, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		log.Printf("failed to parse existing lock pid file (ignoring): %s\n", err)
		return 0, createInstanceLock(lockFilePath)
	}

	// Check if process exists.
	p, err := processInfo.NewProcess(int32(parsedPid))
	switch {
	case err == nil:
		// Process exists, continue.
	case errors.Is(err, processInfo.ErrorProcessNotRunning):
		// A process with the locked PID does not exist.
		// This is expected, so we can continue normally.
		return 0, createInstanceLock(lockFilePath)
	default:
		// There was an internal error getting the process.
		return 0, err
	}

	// Get the process paths and evaluate and clean them.
	executingBinaryPath, err := p.Exe()
	if err != nil {
		return 0, fmt.Errorf("failed to get path of existing process: %w", err)
	}
	cleanedExecutingBinaryPath, err := filepath.EvalSymlinks(executingBinaryPath)
	if err != nil {
		return 0, fmt.Errorf("failed to evaluate path of existing process: %w", err)
	}

	// Check if the binary is portmaster-start with high probability.
	if !strings.Contains(filepath.Base(cleanedExecutingBinaryPath), "portmaster-start") {
		// The process with the locked PID belongs to another binary.
		// As the Portmaster usually starts very early, it will have a low PID,
		// which could be assigned to another process on next boot.
		return 0, createInstanceLock(lockFilePath)
	}

	// Return PID of already running instance.
	return p.Pid, nil
}

func createInstanceLock(lockFilePath string) error {
	// check data root dir
	err := dataRoot.Ensure()
	if err != nil {
		log.Printf("failed to check data root dir: %s\n", err)
	}

	// create lock file
	// TODO: Investigate required permissions.
	err = os.WriteFile(lockFilePath, []byte(strconv.Itoa(os.Getpid())), 0o0666) //nolint:gosec
	if err != nil {
		return err
	}

	return nil
}

func deleteInstanceLock(path, name string, perUser bool) error {
	return os.Remove(getLockFilePath(path, name, perUser))
}

func getLockFilePath(path, name string, perUser bool) string {
	if !perUser {
		return filepath.Join(dataRoot.Path, path, fmt.Sprintf("%s-lock.pid", name))
	}

	// Get user ID for per-user lock file.
	var userID string
	usr, err := user.Current()
	if err != nil {
		log.Printf("failed to get current user: %s\n", err)
		userID = "no-user"
	} else {
		userID = usr.Uid
	}
	return filepath.Join(dataRoot.Path, path, fmt.Sprintf("%s-%s-lock.pid", name, userID))
}
