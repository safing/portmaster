package main

import (
	"errors"
	"log"
	"time"

	"github.com/safing/portmaster/updates"
)

func getFile(opts *Options) (*updates.File, error) {
	// get newest local file
	updates.LoadLatest()

	file, err := updates.GetLocalPlatformFile(opts.Identifier)
	if err == nil {
		return file, nil
	}
	if err != updates.ErrNotFound {
		return nil, err
	}

	// download
	if opts.AllowDownload {
		log.Printf("downloading %s...\n", opts.Identifier)

		// download indexes
		err = updates.UpdateIndexes()
		if err != nil {
			return nil, err
		}

		// download file
		file, err := updates.GetPlatformFile(opts.Identifier)
		if err != nil {
			return nil, err
		}
		return file, nil
	}

	// wait for 30 seconds
	log.Printf("waiting for download of %s (by Portmaster Core) to complete...\n", opts.Identifier)

	// try every 0.5 secs
	for tries := 0; tries < 60; tries++ {
		time.Sleep(500 * time.Millisecond)

		// reload local files
		updates.LoadLatest()

		// get file
		file, err := updates.GetLocalPlatformFile(opts.Identifier)
		if err == nil {
			return file, nil
		}
		if err != updates.ErrNotFound {
			return nil, err
		}
	}
	return nil, errors.New("please try again later or check the Portmaster logs")
}
