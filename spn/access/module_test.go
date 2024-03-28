package access

import (
	"testing"

	"github.com/safing/portmaster/service/core/pmtesting"
	"github.com/safing/portmaster/spn/conf"
)

func TestMain(m *testing.M) {
	conf.EnableClient(true)
	pmtesting.TestMain(m, module)
}
