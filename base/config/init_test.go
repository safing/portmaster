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

func TestConfigPersistence(t *testing.T) {
	t.Parallel()

	instance, err := newTestInstance("test-config")
	if err != nil {
		t.Fatalf("failed to create test instance: %s", err)
	}
	defer func() { _ = os.RemoveAll(instance.DataDir()) }()

	module, err = New(instance)
	if err != nil {
		t.Fatalf("failed to initialize module: %s", err)
	}

	err = SaveConfig()
	if err != nil {
		t.Fatal(err)
	}

	err = loadConfig(true)
	if err != nil {
		t.Fatal(err)
	}
}
