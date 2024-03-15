package navigator

import (
	"testing"

	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/service/core/pmtesting"
)

func TestMain(m *testing.M) {
	log.SetLogLevel(log.DebugLevel)
	pmtesting.TestMain(m, module)
}
