package netutils

import (
	"fmt"
	"net"
	"regexp"
	"strings"

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
			`[a-z0-9_-]{1,63}` + // TLD main chunk with at least one character (for custom ones)
			`\.` + // ending with a dot
			`$`, // match end
	)

	// dnsSDDomainRegex is a lot more lax to better suit the allowed characters in DNS-SD.
	// Not all characters have been allowed - some special characters were
	// removed to reduce the general attack surface.
	dnsSDDomainRegex = regexp.MustCompile(
		// Start of charset selection.
		`^[` +
			// Printable ASCII (character code 32-127), excluding some special characters.
			` !#$%&()*+,\-\./0-9:;=?@A-Z[\\\]^_\a-z{|}~` +
			// Only latin characters from extended ASCII (character code 128-255).
			`ŠŒŽšœžŸ¡¿ÀÁÂÃÄÅÆÇÈÉÊËÌÍÎÏÐÑÒÓÔÕÖØÙÚÛÜÝÞßàáâãäåæçèéêëìíîïðñòóôõöøùúûüýþÿ` +
			// End of charset selection.
			`]*$`,
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

	// IsFqdn checks if a domain name is fully qualified.
	if !dns.IsFqdn(fqdn) {
		return false
	}

	// Use special check for .local domains to support DNS-SD.
	if strings.HasSuffix(fqdn, ".local.") {
		return dnsSDDomainRegex.MatchString(fqdn)
	}

	// check with regex
	if !cleanDomainRegex.MatchString(fqdn) {
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

// IPsToRRs transforms the given IPs to resource records.
func IPsToRRs(domain string, ips []net.IP) ([]dns.RR, error) {
	records := make([]dns.RR, 0, len(ips))
	var rr dns.RR
	var err error

	for _, ip := range ips {
		if ip.To4() != nil {
			rr, err = dns.NewRR(fmt.Sprintf("%s 17 IN A %s", domain, ip))
		} else {
			rr, err = dns.NewRR(fmt.Sprintf("%s 17 IN AAAA %s", domain, ip))
		}
		if err != nil {
			return nil, fmt.Errorf("failed to create record for %s: %w", ip, err)
		}
		records = append(records, rr)
	}

	return records, nil
}
