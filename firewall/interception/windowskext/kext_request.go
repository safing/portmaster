//go:build windows
// +build windows

package windowskext

import "golang.org/x/sys/windows"

const (
	METHOD_BUFFERED   = 0
	METHOD_IN_DIRECT  = 1
	METHOD_OUT_DIRECT = 2
	METHOD_NEITHER    = 3
	SIOCTL_TYPE       = 40000
)

var (
	IOCTL_HELLO                 = ctl_code(SIOCTL_TYPE, 0x800, METHOD_BUFFERED, windows.FILE_READ_DATA|windows.FILE_WRITE_DATA)
	IOCTL_RECV_VERDICT_REQ_POLL = ctl_code(SIOCTL_TYPE, 0x801, METHOD_BUFFERED, windows.FILE_READ_DATA|windows.FILE_WRITE_DATA)
	IOCTL_RECV_VERDICT_REQ      = ctl_code(SIOCTL_TYPE, 0x802, METHOD_BUFFERED, windows.FILE_READ_DATA|windows.FILE_WRITE_DATA)
	IOCTL_SET_VERDICT           = ctl_code(SIOCTL_TYPE, 0x803, METHOD_BUFFERED, windows.FILE_READ_DATA|windows.FILE_WRITE_DATA)
	IOCTL_GET_PAYLOAD           = ctl_code(SIOCTL_TYPE, 0x804, METHOD_BUFFERED, windows.FILE_READ_DATA|windows.FILE_WRITE_DATA)
	IOCTL_CLEAR_CACHE           = ctl_code(SIOCTL_TYPE, 0x805, METHOD_BUFFERED, windows.FILE_READ_DATA|windows.FILE_WRITE_DATA)
	IOCTL_TEST                  = ctl_code(SIOCTL_TYPE, 0x806, METHOD_NEITHER, windows.FILE_READ_DATA|windows.FILE_WRITE_DATA)
)

func ctl_code(device_type, function, method, access uint32) uint32 {
	return (device_type << 16) | (access << 14) | (function << 2) | method
}

func deviceIoControl(code uint32, data *byte, size uintptr) (uint32, error) {
	var bytesReturned uint32
	err := windows.DeviceIoControl(kextHandle,
		code,
		nil, 0,
		data, uint32(size),
		&bytesReturned, nil)

	return bytesReturned, err
}

func deviceIoControlBufferd(code uint32, inData *byte, inSize uintptr, outData *byte, outSize uintptr) (uint32, error) {
	var bytesReturned uint32
	err := windows.DeviceIoControl(kextHandle,
		code,
		inData, uint32(inSize),
		outData, uint32(outSize),
		&bytesReturned, nil)

	return bytesReturned, err
}
