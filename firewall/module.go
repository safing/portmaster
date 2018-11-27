package firewall

import (
	"github.com/Safing/portbase/modules"

	_ "github.com/Safing/portmaster/network"
)

func init() {
	modules.Register("firewall", nil, start, stop, "network")
}

func start() error {
	return registerAsDatabase()
}

func stop() error {

}
