// Copyright Safing ICS Technologies GmbH. Use of this source code is governed by the AGPL license that can be found in the LICENSE file.

package process

import (
	"github.com/Safing/safing-core/database"
	"strings"
	"time"

	datastore "github.com/ipfs/go-datastore"
)

// ExecutableSignature stores a signature of an executable.
type ExecutableSignature []byte

// FileInfo stores (security) information about a file.
type FileInfo struct {
	database.Base
	HumanName      string
	Owners         []string
	ApproxLastSeen int64
	Signature      *ExecutableSignature
}

var fileInfoModel *FileInfo // only use this as parameter for database.EnsureModel-like functions

func init() {
	database.RegisterModel(fileInfoModel, func() database.Model { return new(FileInfo) })
}

// Create saves FileInfo with the provided name in the default namespace.
func (m *FileInfo) Create(name string) error {
	return m.CreateObject(&database.FileInfoCache, name, m)
}

// CreateInNamespace saves FileInfo with the provided name in the provided namespace.
func (m *FileInfo) CreateInNamespace(namespace *datastore.Key, name string) error {
	return m.CreateObject(namespace, name, m)
}

// Save saves FileInfo.
func (m *FileInfo) Save() error {
	return m.SaveObject(m)
}

// getFileInfo fetches FileInfo with the provided name from the default namespace.
func getFileInfo(name string) (*FileInfo, error) {
	return getFileInfoFromNamespace(&database.FileInfoCache, name)
}

// getFileInfoFromNamespace fetches FileInfo with the provided name from the provided namespace.
func getFileInfoFromNamespace(namespace *datastore.Key, name string) (*FileInfo, error) {
	object, err := database.GetAndEnsureModel(namespace, name, fileInfoModel)
	if err != nil {
		return nil, err
	}
	model, ok := object.(*FileInfo)
	if !ok {
		return nil, database.NewMismatchError(object, fileInfoModel)
	}
	return model, nil
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
