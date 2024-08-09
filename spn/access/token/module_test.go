package token

import (
	"fmt"
	"os"
	"testing"

	"github.com/safing/portmaster/base/rng"
)

type testInstance struct{}

func TestMain(m *testing.M) {
	rng, err := rng.New(testInstance{})
	if err != nil {
		fmt.Printf("failed to create RNG module: %s", err)
		os.Exit(1)
	}

	err = rng.Start()
	if err != nil {
		fmt.Printf("failed to start RNG module: %s", err)
		os.Exit(1)
	}
	m.Run()
}
