/*
Package resolver is responsible for querying DNS.

# DNS Servers

Internal lists of resolvers to use are built on start and rebuilt on every config or network change.
Configured DNS servers are prioritized over servers assigned by dhcp. Domain and search options (here referred to as "search scopes") are being considered.

# Security

Usage of DNS Servers can be regulated using the configuration:

	DoNotUseAssignedDNS        // Do not use DNS servers assigned by DHCP
	DoNotUseMDNS               // Do not use mDNS
	DoNotForwardSpecialDomains // Do not forward special domains to local resolvers, except if they have a search scope for it

Note: The DHCP options "domain" and "search" are ignored for servers assigned by DHCP that do not reside within local address space.

# Resolving DNS

Various different queries require the resolver to behave in different manner:

Queries for "localhost." are immediately responded with 127.0.0.1 and ::1, for A and AAAA queries and NXDomain for others.
Reverse lookups on local address ranges (10/8, 172.16/12, 192.168/16, fe80::/7) will be tried against every local resolver and finally mDNS until a successful, non-NXDomain answer is received.
Special domains ("example.", "example.com.", "example.net.", "example.org.", "invalid.", "test.", "onion.") are resolved using search scopes and local resolvers.
All other domains are resolved using search scopes and all available resolvers.
*/
package resolver
