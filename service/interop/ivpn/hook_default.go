//go:build !linux

package ivpn

import (
	"github.com/safing/portmaster/service/mgr"
)

type platformSpecific struct{}

func (i *InteropIvpn) ensureSPNCompatibility(wc *mgr.WorkerCtx) error {
	return nil
}
