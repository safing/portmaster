// Copyright Safing ICS Technologies GmbH. Use of this source code is governed by the AGPL license that can be found in the LICENSE file.

package process

import (
	"strings"
	"sync"
	"time"

	"github.com/Safing/portbase/database/record"
)

// ExecutableSignature stores a signature of an executable.
type ExecutableSignature []byte

// FileInfo stores (security) information about a file.
type FileInfo struct {
	record.Base
	sync.Mutex

	HumanName      string
	Owners         []string
	ApproxLastSeen int64
	Signature      *ExecutableSignature
}

// GetFileInfo gathers information about a file and returns *FileInfo
func GetFileInfo(path string) *FileInfo {
	// TODO: actually get file information
	// TODO: try to load from DB
	// TODO: save to DB (key: hash of some sorts)
	splittedPath := strings.Split("/", path)
	return &FileInfo{
		HumanName:      splittedPath[len(splittedPath)-1],
		ApproxLastSeen: time.Now().Unix(),
	}
}
