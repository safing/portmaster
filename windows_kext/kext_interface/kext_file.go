//go:build windows
// +build windows

package kext_interface

import (
	"golang.org/x/sys/windows"
)

type KextFile struct {
	handle     windows.Handle
	buffer     []byte
	read_slice []byte
}

func (f *KextFile) Read(buffer []byte) (int, error) {
	if f.read_slice == nil || len(f.read_slice) == 0 {
		err := f.refill_read_buffer()
		if err != nil {
			return 0, err
		}
	}

	if len(f.read_slice) >= len(buffer) {
		// Write all requested bytes.
		copy(buffer, f.read_slice[0:len(buffer)])
		f.read_slice = f.read_slice[len(buffer):]
	} else {
		// Write all available bytes and read again.
		copy(buffer[0:len(f.read_slice)], f.read_slice)
		copiedBytes := len(f.read_slice)
		f.read_slice = nil
		_, err := f.Read(buffer[copiedBytes:])
		if err != nil {
			return 0, err
		}
	}

	return len(buffer), nil
}

func (f *KextFile) refill_read_buffer() error {
	var count uint32 = 0
	overlapped := &windows.Overlapped{}
	err := windows.ReadFile(f.handle, f.buffer[:], &count, overlapped)
	if err != nil {
		return err
	}
	f.read_slice = f.buffer[0:count]

	return nil
}

func (f *KextFile) Write(buffer []byte) (int, error) {
	var count uint32 = 0
	overlapped := &windows.Overlapped{}
	err := windows.WriteFile(f.handle, buffer, &count, overlapped)
	return int(count), err
}

func (f *KextFile) Close() error {
	err := windows.CloseHandle(f.handle)
	f.handle = winInvalidHandleValue
	return err
}

func (f *KextFile) deviceIOControl(code uint32, inData []byte, outData []byte) (*windows.Overlapped, error) {
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

	overlapped := &windows.Overlapped{}
	err := windows.DeviceIoControl(f.handle,
		code,
		inDataPtr, inDataSize,
		outDataPtr, outDataSize,
		nil, overlapped)

	if err != nil {
		return nil, err
	}

	return overlapped, nil
}

func (f *KextFile) GetHandle() windows.Handle {
	return f.handle
}
