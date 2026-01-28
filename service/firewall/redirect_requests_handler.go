package firewall

import (
	"net"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/firewall/interception"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/network/packet"
)

func redirectRequestsHandler(w *mgr.WorkerCtx) error {
	for {
		select {
		case <-w.Done():
			return nil
		case rq := <-interception.RedirectRequests:
			handleRedirectRequest(rq)
		}
	}
}

func handleRedirectRequest(req packet.RedirectRequest) {
	var redirectTo *net.IP = nil // nil means no redirect (permit)

	// Defer the reply to ensure it is always sent
	defer func() {
		// Send response back to interception module.
		if err := req.ReplyRedirect(redirectTo); err != nil {
			log.Errorf("failed to reply to redirect request: %s", err)

			// In case of error, it could be that the problem with parameters, so response was not sent to the driver at all.
			// To avoid connection hanging, we try to send a no-redirect response here.
			if err := req.ReplyRedirect(nil); err != nil {
				log.Errorf("failed to reply to redirect request with no-redirect: %s", err)
			}
		}
	}()

	pid := req.ProcessID()
	if pid <= 0 {
		return // Cannot identify process, so cannot apply any profile-based rules.
	}

	// TODO: WIP...

	/*
		// Split-tunneling only applies to TCP and UDP traffic.
		// TODO: Verify whether this check is still needed after the Linux implementation,
		// as the Windows driver only sends RedirectRequest notifications for TCP/UDP.
		switch req.ProtocolType() {
		case packet.TCP, packet.UDP:
		default:
			return
		}

		proc, err := process.GetProcessWithProfile(context.Background(), int(pid))
		if err != nil {
			log.Errorf("redirect request: failed to get process for PID %d: %s", pid, err)
			return
		}

		profile := proc.Profile()
		if profile == nil {
			log.Tracef("redirect request: process PID %d has no profile, cannot apply split-tunneling", pid)
			return
		}
		ifIP := strings.TrimSpace(profile.SplitTunnelInterface())
		if len(ifIP) == 0 {
			return // No split-tunneling interface set.
		}

		// TODO: DELME!!! This is just for testing. The correspond interface address should be used here.
		if req.IsIPv6() {
			return
		}

		ip := net.ParseIP(ifIP)
		if ip == nil {
			log.Tracef("redirect request: process PID %d profile has no split-tunnel interface set, cannot apply split-tunneling", pid)
			return
		}

		fmt.Printf("REDIRECT: %s to '%v'\n", profile.LocalProfile().Name, ip)


		redirectTo = &ip
	*/
}
