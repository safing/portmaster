//go:build !linux && !windows

package ivpn

import (
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/spn/hub"
)

type platformSpecific struct{}

func (i *InteropIvpn) spnConnectingHook(w *mgr.WorkerCtx, hookCtx hub.Announcement) (cancel bool, err error) {
	return true, nil
}
func (i *InteropIvpn) reconcileCompatibilityState(wc *mgr.WorkerCtx) {
}
