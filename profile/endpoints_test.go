package profile

import (
	"net"
	"testing"

	"github.com/Safing/portbase/utils/testutils"
)

func testEndpointDomainMatch(t *testing.T, ep *EndpointPermission, domain string, expectedResult EPResult) {
	var result EPResult
	result, _ = ep.MatchesDomain(domain)
	if result != expectedResult {
		t.Errorf(
			"line %d: unexpected result for endpoint domain match %s: result=%s, expected=%s",
			testutils.GetLineNumberOfCaller(1),
			domain,
			result,
			expectedResult,
		)
	}
}

func testEndpointIPMatch(t *testing.T, ep *EndpointPermission, domain string, ip net.IP, protocol uint8, port uint16, expectedResult EPResult) {
	var result EPResult
	result, _ = ep.MatchesIP(domain, ip, protocol, port, nil)
	if result != expectedResult {
		t.Errorf(
			"line %d: unexpected result for endpoint %s/%s/%d/%d: result=%s, expected=%s",
			testutils.GetLineNumberOfCaller(1),
			domain,
			ip,
			protocol,
			port,
			result,
			expectedResult,
		)
	}
}

func TestEndpointMatching(t *testing.T) {
	ep := &EndpointPermission{
		Type:      EptAny,
		Protocol:  0,
		StartPort: 0,
		EndPort:   0,
		Permit:    true,
	}

	// ANY

	testEndpointDomainMatch(t, ep, "example.com.", Permitted)
	testEndpointIPMatch(t, ep, "example.com.", net.ParseIP("10.2.3.4"), 6, 443, Permitted)

	// DOMAIN

	// wildcard domains
	ep.Type = EptDomain
	ep.Value = "*example.com."
	testEndpointDomainMatch(t, ep, "example.com.", Permitted)
	testEndpointIPMatch(t, ep, "example.com.", net.ParseIP("10.2.3.4"), 6, 443, Permitted)
	testEndpointDomainMatch(t, ep, "abc.example.com.", Permitted)
	testEndpointIPMatch(t, ep, "abc.example.com.", net.ParseIP("10.2.3.4"), 6, 443, Permitted)
	testEndpointDomainMatch(t, ep, "abc-example.com.", Permitted)
	testEndpointIPMatch(t, ep, "abc-example.com.", net.ParseIP("10.2.3.4"), 6, 443, Permitted)

	ep.Value = "*.example.com."
	testEndpointDomainMatch(t, ep, "example.com.", NoMatch)
	testEndpointIPMatch(t, ep, "example.com.", net.ParseIP("10.2.3.4"), 6, 443, NoMatch)
	testEndpointDomainMatch(t, ep, "abc.example.com.", Permitted)
	testEndpointIPMatch(t, ep, "abc.example.com.", net.ParseIP("10.2.3.4"), 6, 443, Permitted)
	testEndpointDomainMatch(t, ep, "abc-example.com.", NoMatch)
	testEndpointIPMatch(t, ep, "abc-example.com.", net.ParseIP("10.2.3.4"), 6, 443, NoMatch)

	ep.Value = ".example.com."
	testEndpointDomainMatch(t, ep, "example.com.", Permitted)
	testEndpointIPMatch(t, ep, "example.com.", net.ParseIP("10.2.3.4"), 6, 443, Permitted)
	testEndpointDomainMatch(t, ep, "abc.example.com.", Permitted)
	testEndpointIPMatch(t, ep, "abc.example.com.", net.ParseIP("10.2.3.4"), 6, 443, Permitted)
	testEndpointDomainMatch(t, ep, "abc-example.com.", NoMatch)
	testEndpointIPMatch(t, ep, "abc-example.com.", net.ParseIP("10.2.3.4"), 6, 443, NoMatch)

	ep.Value = "example.*"
	testEndpointDomainMatch(t, ep, "example.com.", Permitted)
	testEndpointIPMatch(t, ep, "example.com.", net.ParseIP("10.2.3.4"), 6, 443, Permitted)
	testEndpointDomainMatch(t, ep, "abc.example.com.", NoMatch)
	testEndpointIPMatch(t, ep, "abc.example.com.", net.ParseIP("10.2.3.4"), 6, 443, NoMatch)

	ep.Value = ".example.*"
	testEndpointDomainMatch(t, ep, "example.com.", NoMatch)
	testEndpointIPMatch(t, ep, "example.com.", net.ParseIP("10.2.3.4"), 6, 443, NoMatch)
	testEndpointDomainMatch(t, ep, "abc.example.com.", NoMatch)
	testEndpointIPMatch(t, ep, "abc.example.com.", net.ParseIP("10.2.3.4"), 6, 443, NoMatch)

	ep.Value = "*.exampl*"
	testEndpointDomainMatch(t, ep, "abc.example.com.", Permitted)
	testEndpointIPMatch(t, ep, "abc.example.com.", net.ParseIP("10.2.3.4"), 6, 443, Permitted)

	ep.Value = "*.com."
	testEndpointDomainMatch(t, ep, "example.com.", Permitted)
	testEndpointIPMatch(t, ep, "example.com.", net.ParseIP("10.2.3.4"), 6, 443, Permitted)

	// edge case
	ep.Value = ""
	testEndpointDomainMatch(t, ep, "example.com", NoMatch)

	// edge case
	ep.Value = "*"
	testEndpointDomainMatch(t, ep, "example.com", Permitted)

	// edge case
	ep.Value = "**"
	testEndpointDomainMatch(t, ep, "example.com", Permitted)

	// edge case
	ep.Value = "***"
	testEndpointDomainMatch(t, ep, "example.com", Permitted)

	// protocol
	ep.Value = "example.com"
	ep.Protocol = 17
	testEndpointIPMatch(t, ep, "example.com", net.ParseIP("10.2.3.4"), 6, 443, NoMatch)
	testEndpointIPMatch(t, ep, "example.com", net.ParseIP("10.2.3.4"), 17, 443, Permitted)
	testEndpointDomainMatch(t, ep, "example.com", Undeterminable)

	// ports
	ep.StartPort = 442
	ep.EndPort = 444
	testEndpointIPMatch(t, ep, "example.com", net.ParseIP("10.2.3.4"), 17, 80, NoMatch)
	testEndpointIPMatch(t, ep, "example.com", net.ParseIP("10.2.3.4"), 17, 443, Permitted)
	ep.StartPort = 442
	ep.StartPort = 443
	testEndpointIPMatch(t, ep, "example.com", net.ParseIP("10.2.3.4"), 17, 80, NoMatch)
	testEndpointIPMatch(t, ep, "example.com", net.ParseIP("10.2.3.4"), 17, 443, Permitted)
	ep.StartPort = 443
	ep.EndPort = 444
	testEndpointIPMatch(t, ep, "example.com", net.ParseIP("10.2.3.4"), 17, 80, NoMatch)
	testEndpointIPMatch(t, ep, "example.com", net.ParseIP("10.2.3.4"), 17, 443, Permitted)
	ep.StartPort = 443
	ep.EndPort = 443
	testEndpointIPMatch(t, ep, "example.com", net.ParseIP("10.2.3.4"), 17, 80, NoMatch)
	testEndpointIPMatch(t, ep, "example.com", net.ParseIP("10.2.3.4"), 17, 443, Permitted)
	testEndpointDomainMatch(t, ep, "example.com", Undeterminable)

	// IP

	ep.Type = EptIPv4
	ep.Value = "10.2.3.4"
	ep.Protocol = 0
	ep.StartPort = 0
	ep.EndPort = 0
	testEndpointIPMatch(t, ep, "", net.ParseIP("10.2.3.4"), 6, 80, Permitted)
	testEndpointIPMatch(t, ep, "example.com", net.ParseIP("10.2.3.4"), 17, 443, Permitted)
	testEndpointIPMatch(t, ep, "", net.ParseIP("10.2.3.5"), 6, 80, NoMatch)
	testEndpointIPMatch(t, ep, "example.com", net.ParseIP("10.2.3.5"), 17, 443, NoMatch)
	testEndpointDomainMatch(t, ep, "example.com", Undeterminable)
}

func TestEPString(t *testing.T) {
	var endpoints Endpoints
	endpoints = []*EndpointPermission{
		&EndpointPermission{
			Type:     EptDomain,
			Value:    "example.com",
			Protocol: 6,
			Permit:   true,
		},
		&EndpointPermission{
			Type:      EptIPv4,
			Value:     "1.1.1.1",
			Protocol:  17, // TCP
			StartPort: 53, // DNS
			EndPort:   53,
			Permit:    false,
		},
		&EndpointPermission{
			Type:   EptDomain,
			Value:  "example.org",
			Permit: false,
		},
	}
	if endpoints.String() != "[Domain:example.com 6/*, IPv4:1.1.1.1 17/53, Domain:example.org */*]" {
		t.Errorf("unexpected result: %s", endpoints.String())
	}

	var noEndpoints Endpoints
	noEndpoints = []*EndpointPermission{}
	if noEndpoints.String() != "[]" {
		t.Errorf("unexpected result: %s", noEndpoints.String())
	}
}
