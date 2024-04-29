//go:build linux
// +build linux

package kext_interface

type KextFile struct{}

func (f *KextFile) Read(buffer []byte) (int, error) {
	return 0, nil
}

func (f *KextFile) flush_buffer() {}
