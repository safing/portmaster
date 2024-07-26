package access

import (
	"fmt"
	"os"
	"testing"

	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/spn/conf"
)

type testInstance struct {
	config *config.Config
}

func (stub *testInstance) Config() *config.Config {
	return stub.config
}

func (stub *testInstance) SPNGroup() *mgr.ExtendedGroup {
	return nil
}

func (stub *testInstance) Stopping() bool {
	return false
}
func (stub *testInstance) SetCmdLineOperation(f func() error) {}

func TestMain(m *testing.M) {
	instance := &testInstance{}
	var err error
	instance.config, err = config.New(instance)
	if err != nil {
		fmt.Printf("failed to create config module: %s", err)
		os.Exit(0)
	}
	module, err = New(instance)
	if err != nil {
		fmt.Printf("failed to create access module: %s", err)
		os.Exit(0)
	}

	err = instance.config.Start()
	if err != nil {
		fmt.Printf("failed to start config module: %s", err)
		os.Exit(0)
	}
	err = module.Start()
	if err != nil {
		fmt.Printf("failed to start access module: %s", err)
		os.Exit(0)
	}

	conf.EnableClient(true)
	m.Run()
}
