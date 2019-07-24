package process

import (
	"fmt"
	"strconv"
	"testing"
)

func TestGetHumanInfo(t *testing.T) {
	// gather processes for testing
	pids := readDirNames("/proc")
	var allProcs []*Process
	for _, pidString := range pids {
		pid, err := strconv.ParseInt(pidString, 10, 32)
		if err != nil {
			continue
		}
		next, err := GetOrFindProcess(int(pid))
		if err != nil {
			continue
		}
		allProcs = append(allProcs, next)
	}

	// test
	for _, process := range allProcs {
		process.GetHumanInfo()
		fmt.Printf("%d - %s - %s - %s\n", process.Pid, process.Path, process.Name, process.Icon)
	}
}
