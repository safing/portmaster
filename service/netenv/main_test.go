package netenv

import (
	"testing"

	"github.com/safing/portmaster/service/core/pmtesting"
)

func TestMain(m *testing.M) {
	pmtesting.TestMain(m, module)
}
