package api

import (
	"testing"

	"github.com/safing/portmaster/base/config"
)

type testInstance struct {
	config *config.Config
}

var _ instance = &testInstance{}

func (stub *testInstance) Config() *config.Config {
	return stub.config
}

func (stub *testInstance) SetCmdLineOperation(f func() error) {}

func (stub *testInstance) Ready() bool {
	return true
}

func TestMain(m *testing.M) {
	SetDefaultAPIListenAddress("0.0.0.0:8080")
	instance := &testInstance{}
	var err error
	module, err = New(instance)
	if err != nil {
		panic(err)
	}
	err = SetAuthenticator(testAuthenticator)
	if err != nil {
		panic(err)
	}
	m.Run()
}
