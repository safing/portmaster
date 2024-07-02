package dataroot

import (
	"errors"
	"os"

	"github.com/safing/portmaster/base/utils"
)

var root *utils.DirStructure

// Initialize initializes the data root directory.
func Initialize(rootDir string, perm os.FileMode) error {
	if root != nil {
		return errors.New("already initialized")
	}

	root = utils.NewDirStructure(rootDir, perm)
	return root.Ensure()
}

// Root returns the data root directory.
func Root() *utils.DirStructure {
	return root
}
