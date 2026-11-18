//go:build !linux

package ivpn

import (
	"github.com/safing/portmaster/service/mgr"
)

type platformSpecific struct{}

func (i *InteropIvpn) reconcileCompatibilityState(wc *mgr.WorkerCtx) {
}

// clientInstalled reports whether the IVPN client is installed. On these
// platforms the IVPN client library reports a missing installation as
// fs.ErrNotExist, so no separate check is needed.
func clientInstalled() bool {
	return true
}
