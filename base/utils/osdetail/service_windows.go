package osdetail

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Service Status
const (
	StatusUnknown uint8 = iota
	StatusRunningStoppable
	StatusRunningNotStoppable
	StatusStartPending
	StatusStopPending
	StatusStopped
)

// Exported errors
var (
	ErrServiceNotStoppable = errors.New("the service is not stoppable")
)

// GetServiceStatus returns the current status of a Windows Service (limited implementation).
func GetServiceStatus(name string) (status uint8, err error) {

	output, err := exec.Command("sc", "query", name).Output()
	if err != nil {
		return StatusUnknown, fmt.Errorf("failed to query service: %s", err)
	}
	outputString := string(output)

	switch {
	case strings.Contains(outputString, "RUNNING"):
		if strings.Contains(outputString, "NOT_STOPPABLE") {
			return StatusRunningNotStoppable, nil
		}
		return StatusRunningStoppable, nil
	case strings.Contains(outputString, "STOP_PENDING"):
		return StatusStopPending, nil
	case strings.Contains(outputString, "STOPPED"):
		return StatusStopped, nil
	case strings.Contains(outputString, "START_PENDING"):
		return StatusStopPending, nil
	}

	return StatusUnknown, errors.New("unknown service status")
}

// StopService stops a Windows Service.
func StopService(name string) (err error) {
	pendingCnt := 0
	for {

		// get status
		status, err := GetServiceStatus(name)
		if err != nil {
			return err
		}

		switch status {
		case StatusRunningStoppable:
			err := exec.Command("sc", "stop", name).Run()
			if err != nil {
				return fmt.Errorf("failed to stop service: %s", err)
			}
		case StatusRunningNotStoppable:
			return ErrServiceNotStoppable
		case StatusStartPending, StatusStopPending:
			pendingCnt++
			if pendingCnt > 50 {
				return errors.New("service stuck in pending status (5s)")
			}
		case StatusStopped:
			return nil
		}

		time.Sleep(100 * time.Millisecond)
	}
}

// SartService starts a Windows Service.
func SartService(name string) (err error) {
	pendingCnt := 0
	for {

		// get status
		status, err := GetServiceStatus(name)
		if err != nil {
			return err
		}

		switch status {
		case StatusRunningStoppable, StatusRunningNotStoppable:
			return nil
		case StatusStartPending, StatusStopPending:
			pendingCnt++
			if pendingCnt > 50 {
				return errors.New("service stuck in pending status (5s)")
			}
		case StatusStopped:
			err := exec.Command("sc", "start", name).Run()
			if err != nil {
				return fmt.Errorf("failed to stop service: %s", err)
			}
		}

		time.Sleep(100 * time.Millisecond)
	}
}
