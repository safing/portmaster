package osdetail

import (
	"sync"

	"golang.org/x/sys/windows"
)

var (
	colorSupport bool

	colorSupportChecked  bool
	checkingColorSupport sync.Mutex
)

// EnableColorSupport tries to enable color support for cmd on windows and returns whether it is enabled.
func EnableColorSupport() bool {
	checkingColorSupport.Lock()
	defer checkingColorSupport.Unlock()

	if !colorSupportChecked {
		colorSupport = enableColorSupport()
		colorSupportChecked = true
	}
	return colorSupport
}

func enableColorSupport() bool {
	if IsAtLeastWindowsNTVersionWithDefault("10", false) {

		// check if windows.Stdout is file
		if windows.GetFileInformationByHandle(windows.Stdout, &windows.ByHandleFileInformation{}) == nil {
			return false
		}

		var mode uint32
		err := windows.GetConsoleMode(windows.Stdout, &mode)
		if err == nil {
			if mode&windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING == 0 {
				mode |= windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING
				err = windows.SetConsoleMode(windows.Stdout, mode)
				if err != nil {
					return false
				}
			}
			return true
		}
	}

	return false
}
