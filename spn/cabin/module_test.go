package cabin

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

func runTest(m *testing.M) error {
	instance := &testInstance{}
	var err error
	instance.config, err = config.New(instance)
	if err != nil {
		return fmt.Errorf("failed to create config module: %w", err)
	}
	module, err = New(struct{}{})
	if err != nil {
		return fmt.Errorf("failed to create cabin module: %w", err)
	}
	err = instance.config.Start()
	if err != nil {
		return fmt.Errorf("failed to start config module: %w", err)
	}
	err = module.Start()
	if err != nil {
		return fmt.Errorf("failed to start cabin module: %w", err)
	}
	conf.EnablePublicHub(true)

	m.Run()
	return nil
}

func TestMain(m *testing.M) {
	if err := runTest(m); err != nil {
		fmt.Printf("%s\n", err)
		os.Exit(1)
	}
}
