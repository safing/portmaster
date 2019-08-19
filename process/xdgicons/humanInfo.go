package process

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/safing/portbase/log"
)

var (
	humanInfoCache           map[string]*humanInfo
	humanInfoCacheNamesOnly  map[string]*humanInfo
	nextHumanInfoCacheUpdate time.Time

	searchPathCache           map[string]string
	nextSearchPathCacheUpdate time.Time

	humanInfoCacheLock sync.Mutex
)

type humanInfo struct {
	Name     string
	IconName string
	IconPath string
	ExecCmd  string
}

func (m *Process) GetHumanInfo() {
	humanInfoCacheLock.Lock()
	defer humanInfoCacheLock.Unlock()

	// TODO: do some proper fuzzy matching to get these icons!
	// take special care of:
	// - flatpak
	// - snapd

	fuzzyMatch := false
	info, ok := humanInfoCache[m.Path]
	if !ok {
		splitted := strings.Split(m.Path, "/")
		nameOnly := splitted[len(splitted)-1]
		info, ok = humanInfoCacheNamesOnly[nameOnly]
		if ok {
			fuzzyMatch = true
		} else {
			updateHumanInfoCache()
			info, ok = humanInfoCache[m.Path]
			if !ok {
				fuzzyMatch = true
				info, ok = humanInfoCacheNamesOnly[nameOnly]
				if !ok {
					if len(splitted) > 3 {
						info, ok = humanInfoCacheNamesOnly[splitted[len(splitted)-2]]
						if !ok {
							return
						}
					} else {
						return
					}
				}
			}
		}
	}

	if info.IconPath == "" && info.IconName != "" {
		info.IconPath = getIconPathFromName(info.IconName)
	}

	if !fuzzyMatch {
		m.Name = info.Name
	}
	if info.IconPath != "" {
		m.Icon = "f:" + info.IconPath
	}

}

func updateHumanInfoCache() {
	if time.Now().Before(nextHumanInfoCacheUpdate) {
		return
	}
	humanInfoCache = make(map[string]*humanInfo)
	humanInfoCacheNamesOnly = make(map[string]*humanInfo)
	log.Tracef("process: updating human info cache")
	for _, starterDir := range starterLocations {
		for _, dirEntry := range readDirNames(starterDir) {

			// only look at .desktop files
			if !strings.HasSuffix(dirEntry, ".desktop") {
				continue
			}

			execCmd, name, iconPath := getStarterInfo(filepath.Join(starterDir, dirEntry))

			if execCmd == "" {
				continue
			}
			execPath := strings.SplitN(execCmd, " ", 2)[0]

			var ok bool
			if !strings.HasPrefix(execPath, "/") {
				execPath, ok = searchPathCache[execPath]
				if !ok {
					updateSearchPathCache()
					execPath, ok = searchPathCache[execPath]
					if !ok {
						continue
					}
				}
			}

			new := humanInfo{
				Name: name,
			}
			if strings.HasPrefix(iconPath, "/") {
				new.IconPath = iconPath
			} else {
				new.IconName = iconPath
			}
			humanInfoCache[execPath] = &new
			splitted := strings.Split(execPath, "/")
			humanInfoCacheNamesOnly[splitted[len(splitted)-1]] = &new
			// log.Tracef("process: new cache entry: %s - %s - %s - %s", execPath, name, new.IconName, new.IconPath)

		}
	}
	nextHumanInfoCacheUpdate = time.Now().Add(10 * time.Second)
}

func getStarterInfo(filePath string) (execCmd, name, iconPath string) {
	// open file
	file, err := os.Open(filePath)
	if err != nil {
		log.Warningf("process: could not read %s: %s", filePath, err)
		return
	}
	defer file.Close()

	// file scanner
	scanner := bufio.NewScanner(file)
	scanner.Split(bufio.ScanLines)

	// parse
	seen := 0
	scanner.Scan() // skip first line
	for scanner.Scan() {
		line := strings.SplitN(scanner.Text(), "=", 2)
		if len(line) < 2 {
			continue
		}
		switch {
		case line[0] == "Name":
			if name == "" {
				name = line[1]
				seen += 1
			}
		case line[0] == "Exec":
			if execCmd == "" {
				execCmd = line[1]
				seen += 1
			}
		case line[0] == "Icon":
			if iconPath == "" {
				iconPath = line[1]
				seen += 1
			}
		}
		if seen >= 3 {
			return
		}
	}
	return
}

func updateSearchPathCache() {
	if time.Now().Before(nextSearchPathCacheUpdate) {
		return
	}
	searchPathCache = make(map[string]string)
	for _, searchEntry := range strings.Split(os.Getenv("PATH"), ":") {
		names := readDirNames(searchEntry)
		for _, name := range names {
			// only save first occurence
			if _, ok := searchPathCache[name]; !ok {
				searchPathCache[name] = filepath.Join(searchEntry, name)
			}
		}
	}
	nextSearchPathCacheUpdate = time.Now().Add(10 * time.Second)
}

func getIconPathFromName(iconName string) string {
	for _, location := range xdgIconLocations {
		// skip everything that does not exist or we do not have access to.
		if _, err := os.Stat(location); err != nil {
			continue
		}
		// check for icon
		for _, path := range xdgIconPaths {
			possibleIcon := filepath.Join(location, path, iconName+".png")
			if _, err := os.Stat(possibleIcon); err == nil {
				return possibleIcon
			}
		}
	}
	for _, location := range iconLocations {
		possibleIcon := filepath.Join(location, iconName+".png")
		if _, err := os.Stat(possibleIcon); err == nil {
			return possibleIcon
		}
	}
	return ""
}
