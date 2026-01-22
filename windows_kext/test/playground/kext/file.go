//go:build windows
// +build windows

package kext

import (
	"golang.org/x/sys/windows"
)

// KextFile handles communication with the driver
type KextFile struct {
	handle    windows.Handle
	buffer    []byte
	readSlice []byte
}

func (f *KextFile) isValid() bool {
	return f != nil && f.handle != windows.InvalidHandle && f.handle != 0
}

// Read reads data from the driver
func (f *KextFile) Read(buffer []byte) (int, error) {
	if !f.isValid() {
		return 0, ErrFileNotValid
	}

	// If no cached data, read from driver
	if len(f.readSlice) == 0 {
		if err := f.refillBuffer(); err != nil {
			return 0, err
		}
	}

	if len(f.readSlice) >= len(buffer) {
		copy(buffer, f.readSlice[:len(buffer)])
		f.readSlice = f.readSlice[len(buffer):]
		return len(buffer), nil
	}

	// Not enough data - copy what we have and read more
	copied := copy(buffer, f.readSlice)
	f.readSlice = nil
	n, err := f.Read(buffer[copied:])
	return copied + n, err
}

func (f *KextFile) refillBuffer() error {
	var count uint32
	overlapped := &windows.Overlapped{}
	if err := windows.ReadFile(f.handle, f.buffer, &count, overlapped); err != nil {
		return err
	}
	f.readSlice = f.buffer[:count]
	return nil
}

// Write writes data to the driver
func (f *KextFile) Write(buffer []byte) (int, error) {
	if !f.isValid() {
		return 0, ErrFileNotValid
	}
	var count uint32
	overlapped := &windows.Overlapped{}
	if err := windows.WriteFile(f.handle, buffer, &count, overlapped); err != nil {
		return 0, err
	}
	return int(count), nil
}

// Close closes the driver file handle
func (f *KextFile) Close() error {
	if !f.isValid() {
		return nil
	}
	err := windows.CloseHandle(f.handle)
	f.handle = windows.InvalidHandle
	return err
}
