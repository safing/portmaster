package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	processInfo "github.com/shirou/gopsutil/process"
)

func checkAndCreateInstanceLock(name string) (pid int32, err error) {
	lockFilePath := filepath.Join(dataRoot.Path, fmt.Sprintf("%s-lock.pid", name))

	// read current pid file
	data, err := ioutil.ReadFile(lockFilePath)
	if err != nil {
		if os.IsNotExist(err) {
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

	// check if process exists
	p, err := processInfo.NewProcess(int32(parsedPid))
	if err == nil {
		return p.Pid, nil
	}

	// else create new lock
	return 0, createInstanceLock(lockFilePath)
}

func createInstanceLock(lockFilePath string) error {
	// check data root dir
	err := dataRoot.Ensure()
	if err != nil {
		log.Printf("failed to check data root dir: %s\n", err)
	}

	// create lock file
	err = ioutil.WriteFile(lockFilePath, []byte(fmt.Sprintf("%d", os.Getpid())), 0666)
	if err != nil {
		return err
	}

	return nil
}

func deleteInstanceLock(name string) error {
	lockFilePath := filepath.Join(dataRoot.Path, fmt.Sprintf("%s-lock.pid", name))
	return os.Remove(lockFilePath)
}
