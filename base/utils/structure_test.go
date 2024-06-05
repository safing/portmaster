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
	// /repo/b [707]
	// /repo/b/c [750]
	// /repo/b/d [707]
	// /repo/b/d/e [707]
	// /repo/b/d/f [707]
	// /repo/b/d/f/g [707]
	// /repo/b/d/f/g/h [707]
	// /secret [700]

	basePath, err := os.MkdirTemp("", "")
	if err != nil {
		fmt.Println(err)
		return
	}

	ds := NewDirStructure(basePath, 0o0755)
	secret := ds.ChildDir("secret", 0o0700)
	repo := ds.ChildDir("repo", 0o0777)
	_ = repo.ChildDir("a", 0o0700)
	b := repo.ChildDir("b", 0o0707)
	c := b.ChildDir("c", 0o0750)

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
