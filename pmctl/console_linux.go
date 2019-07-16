package main

import "os/exec"

func attachToParentConsole() (attached bool, err error) {
	return true, nil
}

func hideWindow(cmd *exec.Cmd) {
}
