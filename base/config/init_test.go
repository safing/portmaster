package config

import (
	"fmt"
	"os"
	"testing"
)

type testInstance struct{}

var _ instance = testInstance{}

func (stub testInstance) SetCmdLineOperation(f func() error) {}

func runTest(m *testing.M) error {
	ds, err := InitializeUnitTestDataroot("test-config")
	if err != nil {
		return fmt.Errorf("failed to initialize dataroot: %w", err)
	}
	defer func() { _ = os.RemoveAll(ds) }()
	module, err = New(&testInstance{})
	if err != nil {
		return fmt.Errorf("failed to initialize module: %w", err)
	}

	m.Run()
	return nil
}

func TestMain(m *testing.M) {
	if err := runTest(m); err != nil {
		fmt.Printf("%s\n", err)
		os.Exit(1)
	}
}
