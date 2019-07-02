package intel

import (
	"errors"
	"strings"

	"github.com/safing/portbase/log"
	"github.com/miekg/dns"
)

// ResolveIPAndValidate finds (reverse DNS), validates (forward DNS) and returns the domain name assigned to the given IP.
func ResolveIPAndValidate(ip string, securityLevel uint8) (domain string, err error) {
	// get reversed DNS address
	rQ, err := dns.ReverseAddr(ip)
	if err != nil {
		log.Tracef("intel: failed to get reverse address of %s: %s", ip, err)
		return "", err
	}

	// get PTR record
	rrCache := Resolve(nil, rQ, dns.Type(dns.TypePTR), securityLevel)
	if rrCache == nil {
		return "", errors.New("querying for PTR record failed (may be NXDomain)")
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
		return "", errors.New("no PTR record for IP (nxDomain)")
	}

	log.Infof("ptrName: %s", ptrName)

	// get forward record
	if strings.Contains(ip, ":") {
		rrCache = Resolve(nil, ptrName, dns.Type(dns.TypeAAAA), securityLevel)
	} else {
		rrCache = Resolve(nil, ptrName, dns.Type(dns.TypeA), securityLevel)
	}
	if rrCache == nil {
		return "", errors.New("querying for A/AAAA record failed (may be NXDomain)")
	}

	// check for matching A/AAAA record
	log.Infof("rr: %s", rrCache)
	for _, rr := range rrCache.Answer {
		switch v := rr.(type) {
		case *dns.A:
			log.Infof("A: %s", v.A.String())
			if ip == v.A.String() {
				return ptrName, nil
			}
		case *dns.AAAA:
			log.Infof("AAAA: %s", v.AAAA.String())
			if ip == v.AAAA.String() {
				return ptrName, nil
			}
		}
	}

	// no match
	return "", errors.New("validation failed")
}
