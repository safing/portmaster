package interception

import (
	"golang.org/x/sys/windows"

	"github.com/safing/portmaster/base/log"
)

var procDnsFlushResolverCache = windows.NewLazySystemDLL("dnsapi.dll").NewProc("DnsFlushResolverCache")

// flushSystemDNSCache clears the cache of the Windows DNS Client service, like
// "ipconfig /flushdns". Names resolved before the interception started are
// cached there, so apps would reach their IPs without a DNS query that
// Portmaster sees: the connections would have no domain, and domain rules
// would not apply to them until the cache entries expire.
func flushSystemDNSCache() {
	err := procDnsFlushResolverCache.Find()
	if err != nil {
		log.Warningf("interception: cannot flush the DNS Client cache: %s", err)
		return
	}
	ok, _, callErr := procDnsFlushResolverCache.Call()
	if ok == 0 {
		log.Warningf("interception: failed to flush the DNS Client cache: %s", callErr)
		return
	}
	log.Info("interception: flushed the DNS Client cache")
}
