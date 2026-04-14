package interop

import (
	"github.com/safing/portmaster/service/network"
)

func (i *Interoperability) verdict_handler(conn *network.Connection) (verdict network.Verdict, reason string, skipTunnel bool) {
	for _, im := range i.interopModules {
		verdict, reason, skipTunnel := im.VerdictHandler(conn)
		if verdict != network.VerdictUndecided {
			return verdict, reason, skipTunnel
		}
	}
	return network.VerdictUndecided, "", false
}
