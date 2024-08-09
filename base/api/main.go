package api

import (
	"encoding/json"
	"errors"
	"flag"
	"os"
	"time"

	"github.com/safing/portmaster/service/mgr"
)

var exportEndpoints bool

// API Errors.
var (
	ErrAuthenticationAlreadySet = errors.New("the authentication function has already been set")
	ErrAuthenticationImmutable  = errors.New("the authentication function can only be set before the api has started")
)

func init() {
	flag.BoolVar(&exportEndpoints, "export-api-endpoints", false, "export api endpoint registry and exit")
}

func prep() error {
	// Register endpoints.
	if err := registerConfig(); err != nil {
		return err
	}
	if err := registerDebugEndpoints(); err != nil {
		return err
	}
	if err := registerConfigEndpoints(); err != nil {
		return err
	}
	if err := registerMetaEndpoints(); err != nil {
		return err
	}

	if exportEndpoints {
		module.instance.SetCmdLineOperation(exportEndpointsCmd)
		return mgr.ErrExecuteCmdLineOp
	}

	if getDefaultListenAddress() == "" {
		return errors.New("no default listen address for api available")
	}

	return nil
}

func start() error {
	startServer()

	updateAPIKeys()
	module.instance.Config().EventConfigChange.AddCallback("update API keys",
		func(wc *mgr.WorkerCtx, s struct{}) (cancel bool, err error) {
			updateAPIKeys()
			return false, nil
		})

	// start api auth token cleaner
	if authFnSet.IsSet() {
		_ = module.mgr.Repeat("clean api sessions", 5*time.Minute, cleanSessions)
	}

	return registerEndpointBridgeDB()
}

func stop() error {
	return stopServer()
}

func exportEndpointsCmd() error {
	data, err := json.MarshalIndent(ExportEndpoints(), "", "  ")
	if err != nil {
		return err
	}

	_, err = os.Stdout.Write(data)
	return err
}
