package intel

import "strings"

var (
	localReverseScopes = []string{
		".10.in-addr.arpa.",
		".16.172.in-addr.arpa.",
		".17.172.in-addr.arpa.",
		".18.172.in-addr.arpa.",
		".19.172.in-addr.arpa.",
		".20.172.in-addr.arpa.",
		".21.172.in-addr.arpa.",
		".22.172.in-addr.arpa.",
		".23.172.in-addr.arpa.",
		".24.172.in-addr.arpa.",
		".25.172.in-addr.arpa.",
		".26.172.in-addr.arpa.",
		".27.172.in-addr.arpa.",
		".28.172.in-addr.arpa.",
		".29.172.in-addr.arpa.",
		".30.172.in-addr.arpa.",
		".31.172.in-addr.arpa.",
		".168.192.in-addr.arpa.",
		".254.169.in-addr.arpa.",
		".8.e.f.ip6.arpa.",
		".9.e.f.ip6.arpa.",
		".a.e.f.ip6.arpa.",
		".b.e.f.ip6.arpa.",
	}

	// RFC6761, RFC7686
	specialScopes = []string{
		".example.",
		".example.com.",
		".example.net.",
		".example.org.",
		".invalid.",
		".test.",
		".onion.",
	}
)

func domainInScopes(fqdn string, list []string) bool {
	for _, scope := range list {
		if strings.HasSuffix(fqdn, scope) {
			return true
		}
	}
	return false
}
