package resolver

import (
	"context"
	"fmt"
	"strings"

	"github.com/miekg/dns"

	"github.com/safing/portmaster/base/log"
)

// ResolveIPAndValidate finds (reverse DNS), validates (forward DNS) and returns the domain name assigned to the given IP.
func ResolveIPAndValidate(ctx context.Context, ip string) (domain string, err error) {
	// get reversed DNS address
	reverseIP, err := dns.ReverseAddr(ip)
	if err != nil {
		log.Tracer(ctx).Tracef("resolver: failed to get reverse address of %s: %s", ip, err)
		return "", ErrInvalid
	}

	// get PTR record
	q := &Query{
		FQDN:  reverseIP,
		QType: dns.Type(dns.TypePTR),
	}
	rrCache, err := Resolve(ctx, q)
	if err != nil || rrCache == nil {
		return "", fmt.Errorf("failed to resolve %s%s: %w", q.FQDN, q.QType, err)
	}

	// get result from record
	var ptrName string
	for _, rr := range rrCache.Answer {
		ptrRec, ok := rr.(*dns.PTR)
		if ok {
			ptrName = ptrRec.Ptr
			break
		}
	}

	// check for nxDomain
	if ptrName == "" {
		return "", fmt.Errorf("%w: %s%s", ErrNotFound, q.FQDN, q.QType)
	}

	// get forward record
	q = &Query{
		FQDN: ptrName,
	}
	// IPv4/6 switch
	if strings.Contains(ip, ":") {
		q.QType = dns.Type(dns.TypeAAAA)
	} else {
		q.QType = dns.Type(dns.TypeA)
	}
	// resolve
	rrCache, err = Resolve(ctx, q)
	if err != nil || rrCache == nil {
		return "", fmt.Errorf("failed to resolve %s%s: %w", q.FQDN, q.QType, err)
	}

	// check for matching A/AAAA record
	for _, rr := range rrCache.Answer {
		switch v := rr.(type) {
		case *dns.A:
			// log.Debugf("A: %s", v.A.String())
			if ip == v.A.String() {
				return ptrName, nil
			}
		case *dns.AAAA:
			// log.Debugf("AAAA: %s", v.AAAA.String())
			if ip == v.AAAA.String() {
				return ptrName, nil
			}
		}
	}

	// no match
	return "", ErrBlocked
}
