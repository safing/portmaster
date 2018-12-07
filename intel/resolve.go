// Copyright Safing ICS Technologies GmbH. Use of this source code is governed by the AGPL license that can be found in the LICENSE file.

package intel

import (
	"fmt"
	"math/rand"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/miekg/dns"

	"github.com/Safing/portbase/database"
	"github.com/Safing/portbase/log"
	"github.com/Safing/portmaster/network/netutils"
	"github.com/Safing/portmaster/status"
)

// TODO: make resolver interface for http package

// special tlds:

// localhost. [RFC6761] - respond with 127.0.0.1 and ::1 to A and AAAA queries, else nxdomain

// local. [RFC6762] - resolve if search, else resolve with mdns
// 10.in-addr.arpa. [RFC6761]
// 16.172.in-addr.arpa. [RFC6761]
// 17.172.in-addr.arpa. [RFC6761]
// 18.172.in-addr.arpa. [RFC6761]
// 19.172.in-addr.arpa. [RFC6761]
// 20.172.in-addr.arpa. [RFC6761]
// 21.172.in-addr.arpa. [RFC6761]
// 22.172.in-addr.arpa. [RFC6761]
// 23.172.in-addr.arpa. [RFC6761]
// 24.172.in-addr.arpa. [RFC6761]
// 25.172.in-addr.arpa. [RFC6761]
// 26.172.in-addr.arpa. [RFC6761]
// 27.172.in-addr.arpa. [RFC6761]
// 28.172.in-addr.arpa. [RFC6761]
// 29.172.in-addr.arpa. [RFC6761]
// 30.172.in-addr.arpa. [RFC6761]
// 31.172.in-addr.arpa. [RFC6761]
// 168.192.in-addr.arpa. [RFC6761]
// 254.169.in-addr.arpa. [RFC6762]
// 8.e.f.ip6.arpa. [RFC6762]
// 9.e.f.ip6.arpa. [RFC6762]
// a.e.f.ip6.arpa. [RFC6762]
// b.e.f.ip6.arpa. [RFC6762]

// example. [RFC6761] - resolve if search, else return nxdomain
// example.com. [RFC6761] - resolve if search, else return nxdomain
// example.net. [RFC6761] - resolve if search, else return nxdomain
// example.org. [RFC6761] - resolve if search, else return nxdomain
// invalid. [RFC6761] - resolve if search, else return nxdomain
// test. [RFC6761] - resolve if search, else return nxdomain
// onion. [RFC7686] - resolve if search, else return nxdomain

// resolvers:
// local
// global
// mdns

// scopes:
// local-inaddr -> local, mdns
// local -> local scopes, mdns
// global -> local scopes, global
// special -> local scopes, local

// Resolve resolves the given query for a domain and type and returns a RRCache object or nil, if the query failed.
func Resolve(fqdn string, qtype dns.Type, securityLevel uint8) *RRCache {
	fqdn = dns.Fqdn(fqdn)

	// use this to time how long it takes resolve this domain
	// timed := time.Now()
	// defer log.Tracef("intel: took %s to get resolve %s%s", time.Now().Sub(timed).String(), fqdn, qtype.String())

	// check cache
	rrCache, err := GetRRCache(fqdn, qtype)
	if err != nil {
		switch err {
		case database.ErrNotFound:
		default:
			log.Warningf("intel: getting RRCache %s%s from database failed: %s", fqdn, qtype.String(), err)
		}
		return resolveAndCache(fqdn, qtype, securityLevel)
	}

	if rrCache.TTL <= time.Now().Unix() {
		log.Tracef("intel: serving cache, requesting new. TTL=%d, now=%d", rrCache.TTL, time.Now().Unix())
		rrCache.requestingNew = true
		go resolveAndCache(fqdn, qtype, securityLevel)
	}

	// randomize records to allow dumb clients (who only look at the first record) to reliably connect
	for i := range rrCache.Answer {
		j := rand.Intn(i + 1)
		rrCache.Answer[i], rrCache.Answer[j] = rrCache.Answer[j], rrCache.Answer[i]
	}

	return rrCache
}

func resolveAndCache(fqdn string, qtype dns.Type, securityLevel uint8) (rrCache *RRCache) {
	// log.Tracef("intel: resolving %s%s", fqdn, qtype.String())

	// dedup requests
	dupKey := fmt.Sprintf("%s%s", fqdn, qtype.String())
	dupReqLock.Lock()
	mutex, requestActive := dupReqMap[dupKey]
	if !requestActive {
		mutex = new(sync.Mutex)
		mutex.Lock()
		dupReqMap[dupKey] = mutex
		dupReqLock.Unlock()
	} else {
		dupReqLock.Unlock()
		log.Tracef("intel: waiting for duplicate query for %s to complete", dupKey)
		mutex.Lock()
		// wait until duplicate request is finished, then fetch current RRCache and return
		mutex.Unlock()
		var err error
		rrCache, err = GetRRCache(dupKey, qtype)
		if err == nil {
			return rrCache
		}
		// must have been nxdomain if we cannot get RRCache
		return nil
	}
	defer func() {
		dupReqLock.Lock()
		delete(dupReqMap, dupKey)
		dupReqLock.Unlock()
		mutex.Unlock()
	}()

	// resolve
	rrCache = intelligentResolve(fqdn, qtype, securityLevel)
	if rrCache == nil {
		return nil
	}

	// persist to database
	rrCache.Clean(600)
	rrCache.Save()

	return rrCache
}

func intelligentResolve(fqdn string, qtype dns.Type, securityLevel uint8) *RRCache {

	// TODO: handle being offline
	// TODO: handle multiple network connections

	// TODO: handle these in a separate goroutine
	// if config.Changed() {
	// 	log.Info("intel: config changed, reloading resolvers")
	// 	loadResolvers(false)
	// } else if env.NetworkChanged() {
	// 	log.Info("intel: network changed, reloading resolvers")
	// 	loadResolvers(true)
	// }

	resolversLock.RLock()
	defer resolversLock.RUnlock()

	lastFailBoundary := time.Now().Unix() - nameserverRetryRate()
	preDottedFqdn := "." + fqdn

	// resolve:
	// reverse local -> local, mdns
	// local -> local scopes, mdns
	// special -> local scopes, local
	// global -> local scopes, global

	// local reverse scope
	if domainInScopes(preDottedFqdn, localReverseScopes) {
		// try local resolvers
		for _, resolver := range localResolvers {
			rrCache, ok := tryResolver(resolver, lastFailBoundary, fqdn, qtype, securityLevel)
			if ok && rrCache != nil && !rrCache.IsNXDomain() {
				return rrCache
			}
		}
		// check config
		if doNotUseMulticastDNS(securityLevel) {
			return nil
		}
		// try mdns
		rrCache, err := queryMulticastDNS(fqdn, qtype)
		if err != nil {
			log.Errorf("intel: failed to query mdns: %s", err)
		}
		return rrCache
	}

	// local scopes
	for _, scope := range localScopes {
		if strings.HasSuffix(preDottedFqdn, scope.Domain) {
			for _, resolver := range scope.Resolvers {
				rrCache, ok := tryResolver(resolver, lastFailBoundary, fqdn, qtype, securityLevel)
				if ok && rrCache != nil && !rrCache.IsNXDomain() {
					return rrCache
				}
			}
		}
	}

	switch {
	case strings.HasSuffix(preDottedFqdn, ".local."):
		// check config
		if doNotUseMulticastDNS(securityLevel) {
			return nil
		}
		// try mdns
		rrCache, err := queryMulticastDNS(fqdn, qtype)
		if err != nil {
			log.Errorf("intel: failed to query mdns: %s", err)
		}
		return rrCache
	case domainInScopes(preDottedFqdn, specialScopes):
		// check config
		if doNotResolveSpecialDomains(securityLevel) {
			return nil
		}
		// try local resolvers
		for _, resolver := range localResolvers {
			rrCache, ok := tryResolver(resolver, lastFailBoundary, fqdn, qtype, securityLevel)
			if ok {
				return rrCache
			}
		}
	default:
		// try global resolvers
		for _, resolver := range globalResolvers {
			rrCache, ok := tryResolver(resolver, lastFailBoundary, fqdn, qtype, securityLevel)
			if ok {
				return rrCache
			}
		}
	}

	log.Criticalf("intel: failed to resolve %s%s: all resolvers failed (or were skipped to fulfill the security level)", fqdn, qtype.String())
	return nil

	// TODO: check if there would be resolvers available in lower security modes and alert user

}

func tryResolver(resolver *Resolver, lastFailBoundary int64, fqdn string, qtype dns.Type, securityLevel uint8) (*RRCache, bool) {
	// skip if not allowed in current security level
	if resolver.AllowedSecurityLevel < status.CurrentSecurityLevel() || resolver.AllowedSecurityLevel < securityLevel {
		log.Tracef("intel: skipping resolver %s, because it isn't allowed to operate on the current security level: %d|%d", resolver, status.CurrentSecurityLevel(), securityLevel)
		return nil, false
	}
	// skip if not security level denies assigned dns servers
	if doNotUseAssignedNameservers(securityLevel) && resolver.Source == "dhcp" {
		log.Tracef("intel: skipping resolver %s, because assigned nameservers are not allowed on the current security level: %d|%d", resolver, status.CurrentSecurityLevel(), securityLevel)
		return nil, false
	}
	// check if failed recently
	if atomic.LoadInt64(resolver.LastFail) > lastFailBoundary {
		return nil, false
	}
	// TODO: put SkipFqdnBeforeInit back into !resolver.Initialized.IsSet() as soon as Go1.9 arrives and we can use a custom resolver
	// skip resolver if initializing and fqdn is set to skip
	if fqdn == resolver.SkipFqdnBeforeInit {
		return nil, false
	}
	// check if resolver is already initialized
	if !resolver.Initialized.IsSet() {
		// first should init, others wait
		resolver.InitLock.Lock()
		if resolver.Initialized.IsSet() {
			// unlock immediately if resolver was initialized
			resolver.InitLock.Unlock()
		} else {
			// initialize and unlock when finished
			defer resolver.InitLock.Unlock()
		}
		// check if previous init failed
		if atomic.LoadInt64(resolver.LastFail) > lastFailBoundary {
			return nil, false
		}
	}
	// resolve
	log.Tracef("intel: trying to resolve %s%s with %s", fqdn, qtype.String(), resolver.Server)
	rrCache, err := query(resolver, fqdn, qtype)
	if err != nil {
		// check if failing is disabled
		if atomic.LoadInt64(resolver.LastFail) == -1 {
			log.Tracef("intel: non-failing resolver %s failed (%s), moving to next", resolver, err)
			return nil, false
		}
		log.Warningf("intel: resolver %s failed (%s), moving to next", resolver, err)
		resolver.LockReason.Lock()
		resolver.FailReason = err.Error()
		resolver.LockReason.Unlock()
		atomic.StoreInt64(resolver.LastFail, time.Now().Unix())
		resolver.Initialized.UnSet()
		return nil, false
	}
	resolver.Initialized.SetToIf(false, true)

	// remove localhost entries, remove LAN entries if server is in global IP space.
	if resolver.ServerIPScope == netutils.Global {
		rrCache.FilterEntries(true, false, false)
	} else {
		rrCache.FilterEntries(true, true, false)
	}

	return rrCache, true
}

func query(resolver *Resolver, fqdn string, qtype dns.Type) (*RRCache, error) {

	q := new(dns.Msg)
	q.SetQuestion(fqdn, uint16(qtype))

	var reply *dns.Msg
	var err error
	for i := 0; i < 3; i++ {

		// log query time
		// qStart := time.Now()
		reply, _, err = resolver.clientManager.getDNSClient().Exchange(q, resolver.ServerAddress)
		// log.Tracef("intel: query to %s took %s", resolver.Server, time.Now().Sub(qStart))

		// error handling
		if err != nil {
			log.Tracef("intel: query to %s encountered error: %s", resolver.Server, err)

			// TODO: handle special cases
			// 1. connect: network is unreachable
			// 2. timeout

			// temporary error
			if nerr, ok := err.(net.Error); ok && nerr.Timeout() {
				log.Tracef("intel: retrying to resolve %s%s with %s, error was: %s", fqdn, qtype.String(), resolver.Server, err)
				continue
			}

			// permanent error
			break
		}

		// no error
		break
	}

	if err != nil {
		err = fmt.Errorf("resolving %s%s failed: %s", fqdn, qtype.String(), err)
		log.Warning(err.Error())
		return nil, err
	}

	new := &RRCache{
		Domain:   fqdn,
		Question: qtype,
		Answer:   reply.Answer,
		Ns:       reply.Ns,
		Extra:    reply.Extra,
	}

	// TODO: check if reply.Answer is valid
	return new, nil
}
