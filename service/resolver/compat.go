package resolver

import "net"

// This is a workaround for enabling the resolver to work with the compat
// module without importing it. Long-term, the network module should not import
// the resolver package, as this breaks the SPN hub.
var (
	CompatDNSCheckInternalDomainScope string
	CompatSelfCheckIsFailing          func() bool
	CompatSubmitDNSCheckDomain        func(subdomain string) (respondWith net.IP)
)
