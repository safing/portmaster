package service

import "os/exec"

type ServiceConfig struct {
	IsRunningAsService    bool
	DefaultRestartCommand *exec.Cmd
}
