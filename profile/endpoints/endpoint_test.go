package endpoints

import (
	"strings"
	"testing"
)

func TestEndpointParsing(t *testing.T) {
	t.Parallel()

	// any (basics)
	testParsing(t, "- *")
	testParsing(t, "+ *")

	// domain
	testDomainParsing(t, "- *bad*", domainMatchTypeContains, "bad")
	testDomainParsing(t, "- bad*", domainMatchTypePrefix, "bad")
	testDomainParsing(t, "- *bad.com", domainMatchTypeSuffix, "bad.com.")
	testDomainParsing(t, "- .bad.com", domainMatchTypeZone, "bad.com.")
	testDomainParsing(t, "- bad.com", domainMatchTypeExact, "bad.com.")
	testDomainParsing(t, "- www.bad.com.", domainMatchTypeExact, "www.bad.com.")
	testDomainParsing(t, "- www.bad.com", domainMatchTypeExact, "www.bad.com.")

	// ip
	testParsing(t, "+ 127.0.0.1")
	testParsing(t, "+ 192.168.0.1")
	testParsing(t, "+ ::1")
	testParsing(t, "+ 2606:4700:4700::1111")

	// ip
	testParsing(t, "+ 127.0.0.0/8")
	testParsing(t, "+ 192.168.0.0/24")
	testParsing(t, "+ 2606:4700:4700::/48")

	// country
	testParsing(t, "+ DE")
	testParsing(t, "+ AT")
	testParsing(t, "+ CH")
	testParsing(t, "+ AS")

	// asn
	testParsing(t, "+ AS1")
	testParsing(t, "+ AS12")
	testParsing(t, "+ AS123")
	testParsing(t, "+ AS1234")
	testParsing(t, "+ AS12345")

	// network scope
	testParsing(t, "+ Localhost")
	testParsing(t, "+ LAN")
	testParsing(t, "+ Internet")
	testParsing(t, "+ Localhost,LAN,Internet")

	// protocol and ports
	testParsing(t, "+ * TCP/1-1024")
	testParsing(t, "+ * */DNS")
	testParsing(t, "+ * ICMP")
	testParsing(t, "+ * 127")
	testParsing(t, "+ * UDP/1234")
	testParsing(t, "+ * TCP/HTTP")
	testParsing(t, "+ * TCP/80-443")

	// TODO: Test fails:
	// testParsing(t, "+ 1234")
}

func testParsing(t *testing.T, value string) {
	t.Helper()

	ep, err := parseEndpoint(value)
	if err != nil {
		t.Error(err)
		return
	}
	// t.Logf("%T: %+v", ep, ep)
	if value != ep.String() {
		t.Errorf(`stringified endpoint mismatch: original was "%s", parsed is "%s"`, value, ep.String())
	}
}

func testDomainParsing(t *testing.T, value string, matchType uint8, matchValue string) {
	t.Helper()

	testParsing(t, value)

	epGeneric, err := parseTypeDomain(strings.Fields(value))
	if err != nil {
		t.Error(err)
		return
	}
	ep := epGeneric.(*EndpointDomain) //nolint:forcetypeassert

	if ep.MatchType != matchType {
		t.Errorf(`error parsing domain endpoint "%s": match type should be %d, was %d`, value, matchType, ep.MatchType)
	}
	if ep.Domain != matchValue {
		t.Errorf(`error parsing domain endpoint "%s": match domain value should be %s, was %s`, value, matchValue, ep.Domain)
	}
}
