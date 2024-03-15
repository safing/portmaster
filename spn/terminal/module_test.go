package terminal

import (
	"testing"

	"github.com/safing/portmaster/service/core/pmtesting"
	"github.com/safing/portmaster/spn/conf"
)

func TestMain(m *testing.M) {
	conf.EnablePublicHub(true)
	pmtesting.TestMain(m, module)
}
