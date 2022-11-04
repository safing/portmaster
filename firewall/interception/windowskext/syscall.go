//go:build windows
// +build windows

package windowskext

import "golang.org/x/sys/windows"

const (
	METHOD_BUFFERED   = 0
	METHOD_IN_DIRECT  = 1
	METHOD_OUT_DIRECT = 2
	METHOD_NEITHER    = 3

	SIOCTL_TYPE = 40000
)

var (
	IOCTL_VERSION               = ctlCode(SIOCTL_TYPE, 0x800, METHOD_NEITHER, windows.FILE_READ_DATA|windows.FILE_WRITE_DATA)
	IOCTL_RECV_VERDICT_REQ_POLL = ctlCode(SIOCTL_TYPE, 0x801, METHOD_BUFFERED, windows.FILE_READ_DATA|windows.FILE_WRITE_DATA) // Not used
	IOCTL_RECV_VERDICT_REQ      = ctlCode(SIOCTL_TYPE, 0x802, METHOD_BUFFERED, windows.FILE_READ_DATA|windows.FILE_WRITE_DATA)
	IOCTL_SET_VERDICT           = ctlCode(SIOCTL_TYPE, 0x803, METHOD_BUFFERED, windows.FILE_READ_DATA|windows.FILE_WRITE_DATA)
	IOCTL_GET_PAYLOAD           = ctlCode(SIOCTL_TYPE, 0x804, METHOD_BUFFERED, windows.FILE_READ_DATA|windows.FILE_WRITE_DATA)
	IOCTL_CLEAR_CACHE           = ctlCode(SIOCTL_TYPE, 0x805, METHOD_BUFFERED, windows.FILE_READ_DATA|windows.FILE_WRITE_DATA)
	IOCTL_UPDATE_VERDICT        = ctlCode(SIOCTL_TYPE, 0x806, METHOD_NEITHER, windows.FILE_READ_DATA|windows.FILE_WRITE_DATA)
)

func ctlCode(device_type, function, method, access uint32) uint32 {
	return (device_type << 16) | (access << 14) | (function << 2) | method
}

func deviceIoControlRead(handle windows.Handle, code uint32, data []byte) (uint32, error) {
	var bytesReturned uint32

	var dataPtr *byte = nil
	var dataSize uint32 = 0
	if data != nil {
		dataPtr = &data[0]
		dataSize = uint32(len(data))
	}

	err := windows.DeviceIoControl(handle,
		code,
		nil, 0,
		dataPtr, dataSize,
		&bytesReturned, nil)

	return bytesReturned, err
}

func deviceIoControlWrite(handle windows.Handle, code uint32, data []byte) (uint32, error) {
	var bytesReturned uint32

	var dataPtr *byte = nil
	var dataSize uint32 = 0
	if data != nil {
		dataPtr = &data[0]
		dataSize = uint32(len(data))
	}

	err := windows.DeviceIoControl(handle,
		code,
		dataPtr, dataSize,
		nil, 0,
		&bytesReturned, nil)

	return bytesReturned, err
}

func deviceIoControlReadWrite(handle windows.Handle, code uint32, inData []byte, outData []byte) (uint32, error) {
	var bytesReturned uint32

	var inDataPtr *byte = nil
	var inDataSize uint32 = 0
	if inData != nil {
		inDataPtr = &inData[0]
		inDataSize = uint32(len(inData))
	}

	var outDataPtr *byte = nil
	var outDataSize uint32 = 0
	if outData != nil {
		outDataPtr = &outData[0]
		outDataSize = uint32(len(outData))
	}
	err := windows.DeviceIoControl(handle,
		code,
		inDataPtr, inDataSize,
		outDataPtr, outDataSize,
		&bytesReturned, nil)

	return bytesReturned, err
}

// Use for METHOD_NEITHER IOCTL, the data buffer is passed directly to the kernel
func deviceIoControlDirect(handle windows.Handle, code uint32, data []byte) error {
	var dataPtr *byte = nil
	var dataSize uint32 = 0
	if data != nil {
		dataPtr = &data[0]
		dataSize = uint32(len(data))
	}

	err := windows.DeviceIoControl(handle,
		code,
		dataPtr, dataSize,
		nil, 0,
		nil, nil)

	return err
}
