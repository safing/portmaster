package resolver

import (
	"context"
	"fmt"
	"net"
	"strings"

	"github.com/miekg/dns"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/netenv"
	"github.com/safing/portmaster/service/network/netutils"
)

const (
	// InternalSpecialUseDomain is the domain scope used for internal services.
	InternalSpecialUseDomain = "portmaster.home.arpa."

	routerDomain        = "router.local." + InternalSpecialUseDomain
	captivePortalDomain = "captiveportal.local." + InternalSpecialUseDomain
)

var (
	envResolver = &Resolver{
		ConfigURL: ServerSourceEnv,
		Info: &ResolverInfo{
			Type:    ServerTypeEnv,
			Source:  ServerSourceEnv,
			IPScope: netutils.SiteLocal,
		},
		Conn: &envResolverConn{},
	}
	envResolvers = []*Resolver{envResolver}

	internalSpecialUseSOA     dns.RR
	internalSpecialUseComment dns.RR
)

func prepEnvResolver() (err error) {
	netenv.SpecialCaptivePortalDomain = captivePortalDomain

	internalSpecialUseSOA, err = dns.NewRR(fmt.Sprintf(
		"%s 17 IN SOA localhost. none.localhost. 0 0 0 0 0",
		InternalSpecialUseDomain,
	))
	if err != nil {
		return err
	}

	internalSpecialUseComment, err = dns.NewRR(fmt.Sprintf(
		`%s 17 IN TXT "This is a special use TLD of the Portmaster."`,
		InternalSpecialUseDomain,
	))
	return err
}

type envResolverConn struct{}

func (er *envResolverConn) Query(ctx context.Context, q *Query) (*RRCache, error) {
	switch uint16(q.QType) {
	case dns.TypeA, dns.TypeAAAA: // We respond with all IPv4/6 addresses we can find.
		// Check for exact matches.
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

		// Check for suffix matches.
		if strings.HasSuffix(q.FQDN, CompatDNSCheckInternalDomainScope) {
			subdomain := strings.TrimSuffix(q.FQDN, CompatDNSCheckInternalDomainScope)
			respondWith := CompatSubmitDNSCheckDomain(subdomain)

			// We'll get an A record. Only respond if it's an A question.
			if respondWith != nil && uint16(q.QType) == dns.TypeA {
				records, err := netutils.IPsToRRs(q.FQDN, []net.IP{respondWith})
				if err != nil {
					log.Warningf("nameserver: failed to create dns check response to %s: %s", q.FQDN, err)
					return er.nxDomain(q), nil
				}
				return er.makeRRCache(q, records), nil
			}
		}
	case dns.TypeSOA:
		// Direct query for the SOA record.
		if q.FQDN == InternalSpecialUseDomain {
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

	rrCache := &RRCache{
		Domain:   q.FQDN,
		Question: q.QType,
		RCode:    dns.RcodeSuccess,
		Answer:   answers,
		Extra:    []dns.RR{internalSpecialUseComment}, // Always add comment about this TLD.
		Resolver: envResolver.Info.Copy(),
	}
	if len(rrCache.Answer) == 0 {
		rrCache.RCode = dns.RcodeNameError
	}
	return rrCache
}

func (er *envResolverConn) ReportFailure() {}

func (er *envResolverConn) IsFailing() bool {
	return false
}

func (er *envResolverConn) ResetFailure() {}

func (er *envResolverConn) ForceReconnect(_ context.Context) {}

// QueryPortmasterEnv queries the environment resolver directly.
func QueryPortmasterEnv(ctx context.Context, q *Query) (*RRCache, error) {
	return envResolver.Conn.Query(ctx, q)
}
