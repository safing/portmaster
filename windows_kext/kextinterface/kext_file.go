//go:build windows
// +build windows

package kextinterface

import (
	"fmt"

	"golang.org/x/sys/windows"
)

type KextFile struct {
	handle     windows.Handle
	buffer     []byte
	read_slice []byte
}

// Read tries to read the supplied buffer length from the driver.
// The data from the driver is read in chunks `len(f.buffer)` and the extra data is cached for the next call.
// The performance penalty of calling the function with small buffers is very small.
// The function will block until the next info packet is received from the kext.
func (f *KextFile) Read(buffer []byte) (int, error) {
	if err := f.IsValid(); err != nil {
		return 0, fmt.Errorf("failed to read: %w", err)
	}

	// If no data is available from previous calls, read from kext.
	if f.read_slice == nil || len(f.read_slice) == 0 {
		err := f.refill_read_buffer()
		if err != nil {
			return 0, err
		}
	}

	if len(f.read_slice) >= len(buffer) {
		// There is enough data to fill the requested buffer.
		copy(buffer, f.read_slice[0:len(buffer)])
		// Move the slice to contain the remaining data.
		f.read_slice = f.read_slice[len(buffer):]
	} else {
		// There is not enough data to fill the requested buffer.

		// Write everything available.
		copy(buffer[0:len(f.read_slice)], f.read_slice)
		copiedBytes := len(f.read_slice)
		f.read_slice = nil

		// Read again.
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

// Write sends the buffer bytes to the kext. The function will block until the whole buffer is written to the kext.
func (f *KextFile) Write(buffer []byte) (int, error) {
	if err := f.IsValid(); err != nil {
		return 0, fmt.Errorf("failed to write: %w", err)
	}
	var count uint32 = 0
	overlapped := &windows.Overlapped{}
	err := windows.WriteFile(f.handle, buffer, &count, overlapped)
	return int(count), err
}

// Close closes the handle to the kext. This will cancel all active Reads and Writes.
func (f *KextFile) Close() error {
	if err := f.IsValid(); err != nil {
		return fmt.Errorf("failed to close: %w", err)
	}
	err := windows.CloseHandle(f.handle)
	f.handle = windows.InvalidHandle
	return err
}

// deviceIOControl exists for compatibility with the old kext.
func (f *KextFile) deviceIOControl(code uint32, inData []byte, outData []byte) (*windows.Overlapped, error) {
	if err := f.IsValid(); err != nil {
		return nil, fmt.Errorf("failed to send io control: %w", err)
	}
	// Prepare the input data
	var inDataPtr *byte = nil
	var inDataSize uint32 = 0
	if inData != nil {
		inDataPtr = &inData[0]
		inDataSize = uint32(len(inData))
	}

	// Prepare the output data
	var outDataPtr *byte = nil
	var outDataSize uint32 = 0
	if outData != nil {
		outDataPtr = &outData[0]
		outDataSize = uint32(len(outData))
	}

	// Make the request to the kext.
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

// GetHandle returns the handle of the kext.
func (f *KextFile) GetHandle() windows.Handle {
	return f.handle
}

// IsValid checks if kext file holds a valid handle to the kext driver.
func (f *KextFile) IsValid() error {
	if f == nil {
		return fmt.Errorf("nil kext file")
	}

	if f.handle == windows.Handle(0) || f.handle == windows.InvalidHandle {
		return fmt.Errorf("invalid handle")
	}

	return nil
}
