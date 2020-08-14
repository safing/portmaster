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

const (
	internalSpecialUseDomain = "17.home.arpa."

	routerDomain        = "router.local." + internalSpecialUseDomain
	captivePortalDomain = "captiveportal.local." + internalSpecialUseDomain
)

var (
	envResolver = &Resolver{
		Server:        ServerSourceEnv,
		ServerType:    ServerTypeEnv,
		ServerIPScope: netutils.SiteLocal,
		Source:        ServerSourceEnv,
		Conn:          &envResolverConn{},
	}
	envResolvers = []*Resolver{envResolver}

	internalSpecialUseSOA     dns.RR
	internalSpecialUseComment dns.RR
)

func prepEnvResolver() (err error) {
	netenv.SpecialCaptivePortalDomain = captivePortalDomain

	internalSpecialUseSOA, err = dns.NewRR(fmt.Sprintf(
		"%s 17 IN SOA localhost. none.localhost. 0 0 0 0 0",
		internalSpecialUseDomain,
	))
	if err != nil {
		return err
	}

	internalSpecialUseComment, err = dns.NewRR(fmt.Sprintf(
		`%s 17 IN TXT "This is a special use TLD of the Portmaster."`,
		internalSpecialUseDomain,
	))
	return err
}

type envResolverConn struct{}

func (er *envResolverConn) Query(ctx context.Context, q *Query) (*RRCache, error) {
	switch uint16(q.QType) {
	case dns.TypeA, dns.TypeAAAA: // We respond with all IPv4/6 addresses we can find.
		switch q.FQDN {
		case captivePortalDomain:
			// Get IP address of the captive portal.
			portal := netenv.GetCaptivePortal()
			portalIP := portal.IP
			if portalIP == nil {
				portalIP = netenv.PortalTestIP
			}
			// Convert IP to record and respond.
			records, err := netutils.IPsToRRs(q.FQDN, []net.IP{portalIP})
			if err != nil {
				log.Warningf("nameserver: failed to create captive portal response to %s: %s", q.FQDN, err)
				return er.nxDomain(q), nil
			}
			return er.makeRRCache(q, records), nil

		case routerDomain:
			// Get gateways from netenv system.
			routers := netenv.Gateways()
			if len(routers) == 0 {
				return er.nxDomain(q), nil
			}
			// Convert IP to record and respond.
			records, err := netutils.IPsToRRs(q.FQDN, routers)
			if err != nil {
				log.Warningf("nameserver: failed to create gateway response to %s: %s", q.FQDN, err)
				return er.nxDomain(q), nil
			}
			return er.makeRRCache(q, records), nil

		}
	case dns.TypeSOA:
		// Direct query for the SOA record.
		if q.FQDN == internalSpecialUseDomain {
			return er.makeRRCache(q, []dns.RR{internalSpecialUseSOA}), nil
		}
	}

	// No match, reply with NXDOMAIN and SOA record
	reply := er.nxDomain(q)
	reply.Ns = []dns.RR{internalSpecialUseSOA}
	return reply, nil
}

func (er *envResolverConn) nxDomain(q *Query) *RRCache {
	return er.makeRRCache(q, nil)
}

func (er *envResolverConn) makeRRCache(q *Query, answers []dns.RR) *RRCache {
	// Disable caching, as the env always has the raw data available.
	q.NoCaching = true

	return &RRCache{
		Domain:      q.FQDN,
		Question:    q.QType,
		Answer:      answers,
		Extra:       []dns.RR{internalSpecialUseComment}, // Always add comment about this TLD.
		Server:      envResolver.Server,
		ServerScope: envResolver.ServerIPScope,
	}
}

func (er *envResolverConn) ReportFailure() {}

func (er *envResolverConn) IsFailing() bool {
	return false
}
