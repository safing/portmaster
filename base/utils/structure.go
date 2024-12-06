package utils

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"sync"
)

type FSPermission uint8

const (
	AdminOnlyPermission FSPermission = iota
	PublicReadPermission
	PublicWritePermission
)

// AsUnixDirExecPermission return the corresponding unix permission for a directory or executable.
func (perm FSPermission) AsUnixDirExecPermission() fs.FileMode {
	switch perm {
	case AdminOnlyPermission:
		return 0o700
	case PublicReadPermission:
		return 0o755
	case PublicWritePermission:
		return 0o777
	}

	return 0
}

// AsUnixFilePermission return the corresponding unix permission for a regular file.
func (perm FSPermission) AsUnixFilePermission() fs.FileMode {
	switch perm {
	case AdminOnlyPermission:
		return 0o600
	case PublicReadPermission:
		return 0o644
	case PublicWritePermission:
		return 0o666
	}

	return 0
}

// DirStructure represents a directory structure with permissions that should be enforced.
type DirStructure struct {
	sync.Mutex

	Path     string
	Dir      string
	Perm     FSPermission
	Parent   *DirStructure
	Children map[string]*DirStructure
}

// NewDirStructure returns a new DirStructure.
func NewDirStructure(path string, perm FSPermission) *DirStructure {
	return &DirStructure{
		Path:     path,
		Perm:     perm,
		Children: make(map[string]*DirStructure),
	}
}

// ChildDir adds a new child DirStructure and returns it. Should the child already exist, the existing child is returned and the permissions are updated.
func (ds *DirStructure) ChildDir(dirName string, perm FSPermission) (child *DirStructure) {
	ds.Lock()
	defer ds.Unlock()

	// if exists, update
	child, ok := ds.Children[dirName]
	if ok {
		child.Perm = perm
		return child
	}

	// create new
	newDir := &DirStructure{
		Path:     filepath.Join(ds.Path, dirName),
		Dir:      dirName,
		Perm:     perm,
		Parent:   ds,
		Children: make(map[string]*DirStructure),
	}
	ds.Children[dirName] = newDir
	return newDir
}

// Ensure ensures that the specified directory structure (from the first parent on) exists.
func (ds *DirStructure) Ensure() error {
	return ds.EnsureAbsPath(ds.Path)
}

// EnsureRelPath ensures that the specified directory structure (from the first parent on) and the given relative path (to the DirStructure) exists.
func (ds *DirStructure) EnsureRelPath(dirPath string) error {
	return ds.EnsureAbsPath(filepath.Join(ds.Path, dirPath))
}

// EnsureRelDir ensures that the specified directory structure (from the first parent on) and the given relative path (to the DirStructure) exists.
func (ds *DirStructure) EnsureRelDir(dirNames ...string) error {
	return ds.EnsureAbsPath(filepath.Join(append([]string{ds.Path}, dirNames...)...))
}

// EnsureAbsPath ensures that the specified directory structure (from the first parent on) and the given absolute path exists.
// If the given path is outside the DirStructure, an error will be returned.
func (ds *DirStructure) EnsureAbsPath(dirPath string) error {
	// always start at the top
	if ds.Parent != nil {
		return ds.Parent.EnsureAbsPath(dirPath)
	}

	// check if root
	if dirPath == ds.Path {
		return ds.ensure(nil)
	}

	// check scope
	slashedPath := ds.Path
	// add slash to end
	if !strings.HasSuffix(slashedPath, string(filepath.Separator)) {
		slashedPath += string(filepath.Separator)
	}
	// check if given path is in scope
	if !strings.HasPrefix(dirPath, slashedPath) {
		return fmt.Errorf(`path "%s" is outside of DirStructure scope`, dirPath)
	}

	// get relative path
	relPath, err := filepath.Rel(ds.Path, dirPath)
	if err != nil {
		return fmt.Errorf("failed to get relative path: %w", err)
	}

	// split to path elements
	pathDirs := strings.Split(filepath.ToSlash(relPath), "/")

	// start checking
	return ds.ensure(pathDirs)
}

func (ds *DirStructure) ensure(pathDirs []string) error {
	ds.Lock()
	defer ds.Unlock()

	// check current dir
	err := EnsureDirectory(ds.Path, ds.Perm)
	if err != nil {
		return err
	}

	if len(pathDirs) == 0 {
		// we reached the end!
		return nil
	}

	child, ok := ds.Children[pathDirs[0]]
	if !ok {
		// we have reached the end of the defined dir structure
		// ensure all remaining dirs
		dirPath := ds.Path
		for _, dir := range pathDirs {
			dirPath = filepath.Join(dirPath, dir)
			err := EnsureDirectory(dirPath, ds.Perm)
			if err != nil {
				return err
			}
		}
		return nil
	}

	// we got a child, continue
	return child.ensure(pathDirs[1:])
}
