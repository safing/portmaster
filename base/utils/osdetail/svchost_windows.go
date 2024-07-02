package osdetail

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

var (
	serviceNames     map[int32][]string
	serviceNamesLock sync.Mutex
)

// Errors
var (
	ErrServiceNotFound = errors.New("no service with the given PID was found")
)

// GetServiceNames returns all service names assosicated with a svchost.exe process on Windows.
func GetServiceNames(pid int32) ([]string, error) {
	serviceNamesLock.Lock()
	defer serviceNamesLock.Unlock()

	if serviceNames != nil {
		names, ok := serviceNames[pid]
		if ok {
			return names, nil
		}
	}

	serviceNames, err := GetAllServiceNames()
	if err != nil {
		return nil, err
	}

	names, ok := serviceNames[pid]
	if ok {
		return names, nil
	}

	return nil, ErrServiceNotFound
}

// GetAllServiceNames returns a list of service names assosicated with svchost.exe processes on Windows.
func GetAllServiceNames() (map[int32][]string, error) {
	output, err := exec.Command("tasklist", "/svc", "/fi", "imagename eq svchost.exe").Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get svchost tasklist: %s", err)
	}

	// file scanner
	scanner := bufio.NewScanner(bytes.NewReader(output))
	scanner.Split(bufio.ScanLines)

	// skip output header
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), "=") {
			break
		}
	}

	var (
		pid        int32
		services   []string
		collection = make(map[int32][]string)
	)

	for scanner.Scan() {
		// get fields of line
		fields := strings.Fields(scanner.Text())

		// check fields length
		if len(fields) == 0 {
			continue
		}

		// new entry
		if fields[0] == "svchost.exe" {
			// save old entry
			if pid != 0 {
				collection[pid] = services
			}
			// reset PID
			pid = 0
			services = make([]string, 0, len(fields))

			// check fields length
			if len(fields) < 3 {
				continue
			}

			// get pid
			i, err := strconv.ParseInt(fields[1], 10, 32)
			if err != nil {
				continue
			}
			pid = int32(i)

			// skip used fields
			fields = fields[2:]
		}

		// add service names
		for _, field := range fields {
			services = append(services, strings.Trim(strings.TrimSpace(field), ","))
		}
	}

	if pid != 0 {
		// save last entry
		collection[pid] = services
	}

	return collection, nil
}
