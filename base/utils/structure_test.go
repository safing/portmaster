//go:build !windows

package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func ExampleDirStructure() {
	// output:
	// / [755]
	// /repo [777]
	// /repo/b [755]
	// /repo/b/c [777]
	// /repo/b/d [755]
	// /repo/b/d/e [755]
	// /repo/b/d/f [755]
	// /repo/b/d/f/g [755]
	// /repo/b/d/f/g/h [755]
	// /secret [700]

	basePath, err := os.MkdirTemp("", "")
	if err != nil {
		fmt.Println(err)
		return
	}

	ds := NewDirStructure(basePath, PublicReadExecPermission)
	secret := ds.ChildDir("secret", AdminOnlyExecPermission)
	repo := ds.ChildDir("repo", PublicWriteExecPermission)
	_ = repo.ChildDir("a", AdminOnlyExecPermission)
	b := repo.ChildDir("b", PublicReadExecPermission)
	c := b.ChildDir("c", PublicWriteExecPermission)

	err = ds.Ensure()
	if err != nil {
		fmt.Println(err)
	}

	err = c.Ensure()
	if err != nil {
		fmt.Println(err)
	}

	err = secret.Ensure()
	if err != nil {
		fmt.Println(err)
	}

	err = b.EnsureRelDir("d", "e")
	if err != nil {
		fmt.Println(err)
	}

	err = b.EnsureRelPath("d/f/g/h")
	if err != nil {
		fmt.Println(err)
	}

	_ = filepath.Walk(basePath, func(path string, info os.FileInfo, err error) error {
		if err == nil {
			dir := strings.TrimPrefix(path, basePath)
			if dir == "" {
				dir = "/"
			}
			fmt.Printf("%s [%o]\n", dir, info.Mode().Perm())
		}
		return nil
	})
}
