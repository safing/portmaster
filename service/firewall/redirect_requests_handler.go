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

	/*
		fmt.Printf("=================== REDIRECT REQUEST: %v\n", req)

		if req.RemoteAddress().IsLoopback() {
			return
		}

		if req.ProtocolType() != packet.TCP && req.ProtocolType() != packet.UDP {
			return
		}

		if req.RemotePortNumber() == 53 {
			return
		}

		if req.IsIPv6() {
			return
		}

		redirectTo = &redirectAddr

		//if req.LocalAddress().Equal()
	*/
}
