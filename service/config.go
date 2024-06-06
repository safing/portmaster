package service

type ServiceConfig struct {
	ShutdownFunc func(exitCode int)
}
