package main

import (
	"fmt"
	"os"

	"github.com/safing/portmaster/updates"
)

func getFile(identifier string) (*updates.File, error) {
	// get newest local file
	updates.LoadLatest()
	file, err := updates.GetPlatformFile(identifier)
	if err == nil {
		return file, nil
	}
	if err != updates.ErrNotFound {
		return nil, err
	}

	fmt.Printf("%s downloading %s...\n", logPrefix, identifier)

	// if no matching file exists, load index
	err = updates.LoadIndexes()
	if err != nil {
		if os.IsNotExist(err) {
			// create dirs
			err = updates.CheckDir(updateStoragePath)
			if err != nil {
				return nil, err
			}

			// download indexes
			err = updates.CheckForUpdates()
			if err != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
	}

	// get file
	return updates.GetPlatformFile(identifier)
}
