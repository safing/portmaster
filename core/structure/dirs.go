package structure

import (
	"os"

	"github.com/safing/portbase/utils"
)

var (
	root *utils.DirStructure
)

// Initialize initializes the data root directory
func Initialize(rootDir string, perm os.FileMode) error {
	root = utils.NewDirStructure(rootDir, perm)
	return root.Ensure()
}

// Root returns the data root directory.
func Root() *utils.DirStructure {
	return root
}

// NewRootDir calls ChildDir() on the data root directory.
func NewRootDir(dirName string, perm os.FileMode) (childDir *utils.DirStructure) {
	return root.ChildDir(dirName, perm)
}
