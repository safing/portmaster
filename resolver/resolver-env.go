package resolver

import (
	"context"
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

	localSOA dns.RR
)

func prepEnvResolver() (err error) {
	localSOA, err = dns.NewRR("local. 17 IN SOA localhost. none.localhost. 17 17 17 17 17")
	return err
}

type envResolverConn struct{}

func (er *envResolverConn) Query(ctx context.Context, q *Query) (*RRCache, error) {
	// prepping
	portal := netenv.GetCaptivePortal()

	// check for matching name
	switch q.FQDN {
	case "local.":
		// Firefox requests the SOA request for local. before resolving any local. domains.
		// Others might be doing this too. We guessed this behaviour, weren't able to find docs.
		if q.QType == dns.Type(dns.TypeSOA) {
			return er.makeRRCache(q, []dns.RR{localSOA}), nil
		}
		return nil, ErrNotFound

	case netenv.SpecialCaptivePortalDomain:
		portalIP := portal.IP
		if portal.IP == nil {
			portalIP = netenv.PortalTestIP
		}

		records, err := netutils.IPsToRRs(q.FQDN, []net.IP{portalIP})
		if err != nil {
			log.Warningf("nameserver: failed to create captive portal response to %s: %s", q.FQDN, err)
			return nil, ErrNotFound
		}
		return er.makeRRCache(q, records), nil

	case "router.local.":
		routers := netenv.Gateways()
		if len(routers) == 0 {
			return nil, ErrNotFound
		}
		records, err := netutils.IPsToRRs(q.FQDN, routers)
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
