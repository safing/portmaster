package profile

// DEACTIVATED

// import (
// 	"fmt"
// 	"os"
// 	"path/filepath"
// 	"regexp"
// 	"strings"
//
// 	"github.com/safing/portmaster/base/log"
// )
//
// type Framework struct {
// 	// go hirarchy up
// 	FindParent uint8 `json:",omitempty bson:",omitempty"`
// 	// get path from parent, amount of levels to go up the tree (1 means parent, 2 means parent of parents, and so on)
// 	MergeWithParent bool `json:",omitempty bson:",omitempty"`
// 	// instead of getting the path of the parent, merge with it by presenting connections as if they were from that parent
//
// 	// go hirarchy down
// 	Find string `json:",omitempty bson:",omitempty"`
// 	// Regular expression for finding path elements
// 	Build string `json:",omitempty bson:",omitempty"`
// 	// Path definitions for building path
// 	Virtual bool `json:",omitempty bson:",omitempty"`
// 	// Treat resulting path as virtual, do not check if valid
// }
//
// func (f *Framework) GetNewPath(command string, cwd string) (string, error) {
// 	// "/usr/bin/python script"
// 	// to
// 	// "/path/to/script"
// 	regex, err := regexp.Compile(f.Find)
// 	if err != nil {
// 		return "", fmt.Errorf("profiles(framework): failed to compile framework regex: %s", err)
// 	}
// 	matched := regex.FindAllStringSubmatch(command, -1)
// 	if len(matched) == 0 || len(matched[0]) < 2 {
// 		return "", fmt.Errorf("profiles(framework): regex \"%s\" for constructing path did not match command \"%s\"", f.Find, command)
// 	}
//
// 	var lastError error
// 	var buildPath string
// 	for _, buildPath = range strings.Split(f.Build, "|") {
//
// 		buildPath = strings.Replace(buildPath, "{CWD}", cwd, -1)
// 		for i := 1; i < len(matched[0]); i++ {
// 			buildPath = strings.Replace(buildPath, fmt.Sprintf("{%d}", i), matched[0][i], -1)
// 		}
//
// 		buildPath = filepath.Clean(buildPath)
//
// 		if !f.Virtual {
// 			if !strings.HasPrefix(buildPath, "~/") && !filepath.IsAbs(buildPath) {
// 				lastError = fmt.Errorf("constructed path \"%s\" from framework is not absolute", buildPath)
// 				continue
// 			}
// 			if _, err := os.Stat(buildPath); errors.Is(err, fs.ErrNotExist) {
// 				lastError = fmt.Errorf("constructed path \"%s\" does not exist", buildPath)
// 				continue
// 			}
// 		}
//
// 		lastError = nil
// 		break
//
// 	}
//
// 	if lastError != nil {
// 		return "", fmt.Errorf("profiles(framework): failed to construct valid path, last error: %s", lastError)
// 	}
// 	log.Tracef("profiles(framework): transformed \"%s\" (%s) to \"%s\"", command, cwd, buildPath)
// 	return buildPath, nil
// }
