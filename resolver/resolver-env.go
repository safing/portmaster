package resolver

import (
	"context"
	"fmt"
	"net"

	"github.com/miekg/dns"
	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/netenv"
	"github.com/safing/portmaster/network/netutils"
)

var (
	envResolver = &Resolver{
		Server:        ServerSourceEnv,
		ServerType:    ServerTypeEnv,
		ServerIPScope: netutils.SiteLocal,
		Source:        ServerSourceEnv,
		Conn:          &envResolverConn{},
	}
)

type envResolverConn struct{}

func (er *envResolverConn) Query(ctx context.Context, q *Query) (*RRCache, error) {
	// prepping
	portal := netenv.GetCaptivePortal()

	// check for matching name
	switch q.FQDN {
	case netenv.SpecialCaptivePortalDomain:
		if portal.IP != nil {
			rr, err := portal.IPasRR()
			if err != nil {
				log.Warningf("nameserver: failed to create captive portal response to %s: %s", q.FQDN, err)
				return nil, ErrNotFound
			}
			return er.makeRRCache(q, []dns.RR{rr}), nil
		}
		return nil, ErrNotFound

	case "router.local.":
		routers := netenv.Gateways()
		if len(routers) == 0 {
			return nil, ErrNotFound
		}
		records, err := ipsToRRs(q.FQDN, routers)
		if err != nil {
			log.Warningf("nameserver: failed to create gateway response to %s: %s", q.FQDN, err)
			return nil, ErrNotFound
		}
		return er.makeRRCache(q, records), nil
	}

	// no match
	return nil, ErrContinue // continue with next resolver
}

func (er *envResolverConn) makeRRCache(q *Query, answers []dns.RR) *RRCache {
	q.NoCaching = true // disable caching, as the env always has the data available and more up to date.
	return &RRCache{
		Domain:      q.FQDN,
		Question:    q.QType,
		Answer:      answers,
		Server:      envResolver.Server,
		ServerScope: envResolver.ServerIPScope,
	}
}

func (er *envResolverConn) ReportFailure() {}

func (er *envResolverConn) IsFailing() bool {
	return false
}

func ipsToRRs(domain string, ips []net.IP) ([]dns.RR, error) {
	var records []dns.RR
	var rr dns.RR
	var err error

	for _, ip := range ips {
		if ip.To4() != nil {
			rr, err = dns.NewRR(domain + " 17 IN A " + ip.String())
		} else {
			rr, err = dns.NewRR(domain + " 17 IN AAAA " + ip.String())
		}
		if err != nil {
			return nil, fmt.Errorf("failed to create record for %s: %w", ip, err)
		}
		records = append(records, rr)
	}

	return records, nil
}
