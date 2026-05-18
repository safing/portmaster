//go:build windows

package ivpn

import (
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/netenv"
	"github.com/safing/portmaster/spn/conf"
	"github.com/safing/portmaster/spn/hub"
)

type platformSpecific struct{}

func (i *InteropIvpn) spnConnectingHook(w *mgr.WorkerCtx, hookCtx hub.Announcement) (cancel bool, err error) {

	// Bind SPN outgoing connections to the physical (non-VPN) interface so that
	// SPN hub traffic bypasses the IVPN tunnel. Without this, the OS would route
	// SPN connections through the VPN interface, defeating the split-tunnel design.
	//
	// TODO: netenv.GetDefaultInterface() is a heuristic and may not always return
	// the correct physical interface (e.g. with multiple uplinks or unusual routing).
	defIf := netenv.GetDefaultInterface()
	if defIf == nil {
		conf.SetBindAddr(nil, nil)
	} else {
		conf.SetBindAddr(defIf.IPv4Address, defIf.IPv6Address)
	}

	return false, nil
}
func (i *InteropIvpn) reconcileCompatibilityState(wc *mgr.WorkerCtx) {
}
