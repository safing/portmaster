package nameserver

import (
	"github.com/miekg/dns"
	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/nameserver/nsutil"
	"github.com/safing/portmaster/network"
)

// sendResponse sends a response to query using w. If reasonCtx is not
// nil and implements either the Responder or RRProvider interface then
// those functions are used to craft a DNS response. If reasonCtx is nil
// or does not implement the Responder interface and verdict is not set
// to failed a ZeroIP response will be sent. If verdict is set to failed
// then a ServFail will be sent instead.
func sendResponse(w dns.ResponseWriter, query *dns.Msg, verdict network.Verdict, reason string, reasonCtx interface{}) {
	responder, ok := reasonCtx.(nsutil.Responder)
	if !ok {
		if verdict == network.VerdictFailed {
			responder = nsutil.ServeFail()
		} else {
			responder = nsutil.ZeroIP()
		}
	}

	reply := responder.ReplyWithDNS(query, reason, reasonCtx)

	if extra, ok := reasonCtx.(nsutil.RRProvider); ok {
		rrs := extra.GetExtraRR(query, reason, reasonCtx)
		reply.Extra = append(reply.Extra, rrs...)
	}

	if err := w.WriteMsg(reply); err != nil {
		log.Errorf("nameserver: failed to send response: %s", err)
	}
}
