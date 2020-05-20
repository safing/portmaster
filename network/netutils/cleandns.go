package netutils

import (
	"regexp"

	"github.com/miekg/dns"
)

var (
	cleanDomainRegex = regexp.MustCompile(
		`^` + // match beginning
			`(` + // start subdomain group
			`(xn--)?` + // idn prefix
			`[a-z0-9_-]{1,63}` + // main chunk
			`\.` + // ending with a dot
			`)*` + // end subdomain group, allow any number of subdomains
			`(xn--)?` + // TLD idn prefix
			`[a-z0-9_-]{2,63}` + // TLD main chunk with at least two characters
			`\.` + // ending with a dot
			`$`, // match end
	)
)

// IsValidFqdn returns whether the given string is a valid fqdn.
func IsValidFqdn(fqdn string) bool {
	// root zone
	if fqdn == "." {
		return true
	}

	// check max length
	if len(fqdn) > 256 {
		return false
	}

	// check with regex
	if !cleanDomainRegex.MatchString(fqdn) {
		return false
	}

	// check with miegk/dns

	// IsFqdn checks if a domain name is fully qualified.
	if !dns.IsFqdn(fqdn) {
		return false
	}

	// IsDomainName checks if s is a valid domain name, it returns the number of
	// labels and true, when a domain name is valid.  Note that non fully qualified
	// domain name is considered valid, in this case the last label is counted in
	// the number of labels.  When false is returned the number of labels is not
	// defined.  Also note that this function is extremely liberal; almost any
	// string is a valid domain name as the DNS is 8 bit protocol. It checks if each
	// label fits in 63 characters and that the entire name will fit into the 255
	// octet wire format limit.
	_, ok := dns.IsDomainName(fqdn)
	return ok
}
