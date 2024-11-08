package config

import (
	"fmt"
	"os"
	"testing"
)

type testInstance struct {
	dataDir string
}

var _ instance = testInstance{}

func (stub testInstance) DataDir() string {
	return stub.dataDir
}

func (stub testInstance) SetCmdLineOperation(f func() error) {}

func newTestInstance(testName string) (*testInstance, error) {
	testDir, err := os.MkdirTemp("", fmt.Sprintf("portmaster-%s", testName))
	if err != nil {
		return nil, fmt.Errorf("failed to make tmp dir: %w", err)
	}

	return &testInstance{
		dataDir: testDir,
	}, nil
}

func TestMain(m *testing.M) {
	instance, err := newTestInstance("test-config")
	if err != nil {
		panic(fmt.Errorf("failed to create test instance: %w", err))
	}
	defer func() { _ = os.RemoveAll(instance.DataDir()) }()

	module, err = New(instance)
	if err != nil {
		panic(fmt.Errorf("failed to initialize module: %w", err))
	}

	m.Run()
}

func TestConfigPersistence(t *testing.T) { //nolint:paralleltest
	err := SaveConfig()
	if err != nil {
		t.Fatal(err)
	}

	err = loadConfig(true)
	if err != nil {
		t.Fatal(err)
	}
}
