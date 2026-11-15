//go:build windows

package ivpn

import (
	"github.com/safing/portmaster/service/mgr"
)

type platformSpecific struct{}

func (i *InteropIvpn) reconcileCompatibilityState(wc *mgr.WorkerCtx) {
}
