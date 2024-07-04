//go:build windows
// +build windows

package kextinterface

import (
	"golang.org/x/sys/windows"
)

const (
	METHOD_BUFFERED   = 0
	METHOD_IN_DIRECT  = 1
	METHOD_OUT_DIRECT = 2
	METHOD_NEITHER    = 3

	SIOCTL_TYPE = 40000
)

func ctlCode(device_type, function, method, access uint32) uint32 {
	return (device_type << 16) | (access << 14) | (function << 2) | method
}

var (
	IOCTL_VERSION          = ctlCode(SIOCTL_TYPE, 0x800, METHOD_BUFFERED, windows.FILE_READ_DATA|windows.FILE_WRITE_DATA)
	IOCTL_SHUTDOWN_REQUEST = ctlCode(SIOCTL_TYPE, 0x801, METHOD_BUFFERED, windows.FILE_READ_DATA|windows.FILE_WRITE_DATA)
)

func ReadVersion(file *KextFile) ([]uint8, error) {
	data := make([]uint8, 4)
	_, err := file.deviceIOControl(IOCTL_VERSION, nil, data)
	if err != nil {
		return nil, err
	}
	return data, nil
}
