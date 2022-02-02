package resolver

import (
	"net"

	"github.com/miekg/dns"
)

// Supported upstream block detections.
const (
	BlockDetectionRefused     = "refused"
	BlockDetectionZeroIP      = "zeroip"
	BlockDetectionEmptyAnswer = "empty"
	BlockDetectionDisabled    = "disabled"
)

func isBlockedUpstream(resolver *Resolver, answer *dns.Msg) bool {
	if resolver.UpstreamBlockDetection == BlockDetectionDisabled {
		return false
	}

	switch resolver.UpstreamBlockDetection {
	case BlockDetectionRefused:
		return answer.Rcode == dns.RcodeRefused
	case BlockDetectionZeroIP:
		if answer.Rcode != dns.RcodeSuccess {
			return false
		}
		var ips []net.IP
		for _, rr := range answer.Answer {
			switch v := rr.(type) {
			case *dns.A:
				ips = append(ips, v.A)
			case *dns.AAAA:
				ips = append(ips, v.AAAA)
			}
		}

		if len(ips) == 0 {
			return false // we expected an empty IP
		}

		for _, ip := range ips {
			if ip.To4() != nil {
				if !ip.Equal(net.IPv4zero) {
					return false
				}
			} else {
				if !ip.To16().Equal(net.IPv6zero) {
					return false
				}
			}
		}

		return true
	case BlockDetectionEmptyAnswer:
		return answer.Rcode == dns.RcodeNameError && len(answer.Ns) == 0 && len(answer.Answer) == 0 && len(answer.Extra) == 0
	}

	return false
}
