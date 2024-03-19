//go:build !windows
// +build !windows

package main

func shouldRunService() bool {
	return false
}

func runService() int {
	return 0
}
