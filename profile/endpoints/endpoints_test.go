package endpoints

import (
	"net"
	"runtime"
	"testing"

	"github.com/safing/portmaster/core/pmtesting"
	"github.com/safing/portmaster/intel"
)

func TestMain(m *testing.M) {
	pmtesting.TestMain(m, intel.Module)
}

func testEndpointMatch(t *testing.T, ep Endpoint, entity *intel.Entity, expectedResult EPResult) {
	result, _ := ep.Matches(entity)
	if result != expectedResult {
		t.Errorf(
			"line %d: unexpected result for endpoint %s and entity %+v: result=%s, expected=%s",
			getLineNumberOfCaller(1),
			ep,
			entity,
			result,
			expectedResult,
		)
	}
}

func TestEndpointMatching(t *testing.T) {
	// ANY

	ep, err := parseEndpoint("+ *")
	if err != nil {
		t.Fatal(err)
	}

	testEndpointMatch(t, ep, (&intel.Entity{
		Domain: "example.com.",
	}).Init(), Permitted)
	testEndpointMatch(t, ep, (&intel.Entity{
		Domain:   "example.com.",
		IP:       net.ParseIP("10.2.3.4"),
		Protocol: 6,
		Port:     443,
	}).Init(), Permitted)

	// DOMAIN

	// wildcard domains
	ep, err = parseEndpoint("+ *example.com")
	if err != nil {
		t.Fatal(err)
	}

	testEndpointMatch(t, ep, (&intel.Entity{
		Domain: "example.com.",
	}).Init(), Permitted)
	testEndpointMatch(t, ep, (&intel.Entity{
		Domain: "abc.example.com.",
	}).Init(), Permitted)
	testEndpointMatch(t, ep, (&intel.Entity{
		Domain: "abc-example.com.",
	}).Init(), Permitted)
	testEndpointMatch(t, ep, (&intel.Entity{
		Domain:   "example.com.",
		IP:       net.ParseIP("10.2.3.4"),
		Protocol: 6,
		Port:     443,
	}).Init(), Permitted)
	testEndpointMatch(t, ep, (&intel.Entity{
		Domain:   "abc.example.com.",
		IP:       net.ParseIP("10.2.3.4"),
		Protocol: 6,
		Port:     443,
	}).Init(), Permitted)
	testEndpointMatch(t, ep, (&intel.Entity{
		Domain:   "abc-example.com.",
		IP:       net.ParseIP("10.2.3.4"),
		Protocol: 6,
		Port:     443,
	}).Init(), Permitted)

	ep, err = parseEndpoint("+ *.example.com")
	if err != nil {
		t.Fatal(err)
	}

	testEndpointMatch(t, ep, (&intel.Entity{
		Domain: "example.com.",
	}).Init(), NoMatch)
	testEndpointMatch(t, ep, (&intel.Entity{
		Domain: "abc.example.com.",
	}).Init(), Permitted)
	testEndpointMatch(t, ep, (&intel.Entity{
		Domain: "abc-example.com.",
	}).Init(), NoMatch)
	testEndpointMatch(t, ep, (&intel.Entity{
		Domain:   "example.com.",
		IP:       net.ParseIP("10.2.3.4"),
		Protocol: 6,
		Port:     443,
	}).Init(), NoMatch)
	testEndpointMatch(t, ep, (&intel.Entity{
		Domain:   "abc.example.com.",
		IP:       net.ParseIP("10.2.3.4"),
		Protocol: 6,
		Port:     443,
	}).Init(), Permitted)
	testEndpointMatch(t, ep, (&intel.Entity{
		Domain:   "abc-example.com.",
		IP:       net.ParseIP("10.2.3.4"),
		Protocol: 6,
		Port:     443,
	}).Init(), NoMatch)

	ep, err = parseEndpoint("+ .example.com")
	if err != nil {
		t.Fatal(err)
	}

	testEndpointMatch(t, ep, (&intel.Entity{
		Domain: "example.com.",
	}).Init(), Permitted)
	testEndpointMatch(t, ep, (&intel.Entity{
		Domain: "abc.example.com.",
	}).Init(), Permitted)
	testEndpointMatch(t, ep, (&intel.Entity{
		Domain: "abc-example.com.",
	}).Init(), NoMatch)
	testEndpointMatch(t, ep, (&intel.Entity{
		Domain:   "example.com.",
		IP:       net.ParseIP("10.2.3.4"),
		Protocol: 6,
		Port:     443,
	}).Init(), Permitted)
	testEndpointMatch(t, ep, (&intel.Entity{
		Domain:   "abc.example.com.",
		IP:       net.ParseIP("10.2.3.4"),
		Protocol: 6,
		Port:     443,
	}).Init(), Permitted)
	testEndpointMatch(t, ep, (&intel.Entity{
		Domain:   "abc-example.com.",
		IP:       net.ParseIP("10.2.3.4"),
		Protocol: 6,
		Port:     443,
	}).Init(), NoMatch)

	ep, err = parseEndpoint("+ example.*")
	if err != nil {
		t.Fatal(err)
	}

	testEndpointMatch(t, ep, (&intel.Entity{
		Domain: "example.com.",
	}).Init(), Permitted)
	testEndpointMatch(t, ep, (&intel.Entity{
		Domain: "abc.example.com.",
	}).Init(), NoMatch)
	testEndpointMatch(t, ep, (&intel.Entity{
		Domain:   "example.com.",
		IP:       net.ParseIP("10.2.3.4"),
		Protocol: 6,
		Port:     443,
	}).Init(), Permitted)
	testEndpointMatch(t, ep, (&intel.Entity{
		Domain:   "abc.example.com.",
		IP:       net.ParseIP("10.2.3.4"),
		Protocol: 6,
		Port:     443,
	}).Init(), NoMatch)

	ep, err = parseEndpoint("+ *.exampl*")
	if err != nil {
		t.Fatal(err)
	}

	testEndpointMatch(t, ep, (&intel.Entity{
		Domain: "example.com.",
	}).Init(), NoMatch)
	testEndpointMatch(t, ep, (&intel.Entity{
		Domain: "abc.example.com.",
	}).Init(), Permitted)
	testEndpointMatch(t, ep, (&intel.Entity{
		Domain:   "example.com.",
		IP:       net.ParseIP("10.2.3.4"),
		Protocol: 6,
		Port:     443,
	}).Init(), NoMatch)
	testEndpointMatch(t, ep, (&intel.Entity{
		Domain:   "abc.example.com.",
		IP:       net.ParseIP("10.2.3.4"),
		Protocol: 6,
		Port:     443,
	}).Init(), Permitted)

	ep, err = parseEndpoint("+ *.com.")
	if err != nil {
		t.Fatal(err)
	}

	testEndpointMatch(t, ep, (&intel.Entity{
		Domain: "example.com.",
	}).Init(), Permitted)
	testEndpointMatch(t, ep, (&intel.Entity{
		Domain: "example.org.",
	}).Init(), NoMatch)

	// protocol

	ep, err = parseEndpoint("+ example.com UDP")
	if err != nil {
		t.Fatal(err)
	}

	testEndpointMatch(t, ep, (&intel.Entity{
		Domain:   "example.com.",
		IP:       net.ParseIP("10.2.3.4"),
		Protocol: 6,
		Port:     443,
	}).Init(), NoMatch)
	testEndpointMatch(t, ep, (&intel.Entity{
		Domain:   "example.com.",
		IP:       net.ParseIP("10.2.3.4"),
		Protocol: 17,
		Port:     443,
	}).Init(), Permitted)
	testEndpointMatch(t, ep, (&intel.Entity{
		Domain: "example.com.",
	}).Init(), Undeterminable)

	// ports

	ep, err = parseEndpoint("+ example.com 17/442-444")
	if err != nil {
		t.Fatal(err)
	}

	entity := (&intel.Entity{
		Domain:   "example.com.",
		IP:       net.ParseIP("10.2.3.4"),
		Protocol: 17,
		Port:     441,
	}).Init()
	testEndpointMatch(t, ep, entity, NoMatch)

	entity.Port = 442
	testEndpointMatch(t, ep, entity, Permitted)

	entity.Port = 443
	testEndpointMatch(t, ep, entity, Permitted)

	entity.Port = 444
	testEndpointMatch(t, ep, entity, Permitted)

	entity.Port = 445
	testEndpointMatch(t, ep, entity, NoMatch)

	testEndpointMatch(t, ep, (&intel.Entity{
		Domain: "example.com.",
	}).Init(), Undeterminable)

	// IP

	ep, err = parseEndpoint("+ 10.2.3.4")
	if err != nil {
		t.Fatal(err)
	}

	testEndpointMatch(t, ep, (&intel.Entity{
		Domain:   "",
		IP:       net.ParseIP("10.2.3.4"),
		Protocol: 6,
		Port:     443,
	}).Init(), Permitted)
	testEndpointMatch(t, ep, (&intel.Entity{
		Domain:   "example.com.",
		IP:       net.ParseIP("10.2.3.4"),
		Protocol: 17,
		Port:     443,
	}).Init(), Permitted)

	testEndpointMatch(t, ep, (&intel.Entity{
		Domain:   "",
		IP:       net.ParseIP("10.2.3.3"),
		Protocol: 6,
		Port:     443,
	}).Init(), NoMatch)
	testEndpointMatch(t, ep, (&intel.Entity{
		Domain:   "example.com.",
		IP:       net.ParseIP("10.2.3.5"),
		Protocol: 17,
		Port:     443,
	}).Init(), NoMatch)

	testEndpointMatch(t, ep, (&intel.Entity{
		Domain: "example.com.",
	}).Init(), Undeterminable)

	// IP Range

	ep, err = parseEndpoint("+ 10.2.3.0/24")
	if err != nil {
		t.Fatal(err)
	}
	testEndpointMatch(t, ep, (&intel.Entity{
		IP: net.ParseIP("10.2.2.4"),
	}).Init(), NoMatch)
	testEndpointMatch(t, ep, (&intel.Entity{
		IP: net.ParseIP("10.2.3.4"),
	}).Init(), Permitted)
	testEndpointMatch(t, ep, (&intel.Entity{
		IP: net.ParseIP("10.2.4.4"),
	}).Init(), NoMatch)

	// ASN

	ep, err = parseEndpoint("+ AS13335")
	if err != nil {
		t.Fatal(err)
	}

	testEndpointMatch(t, ep, (&intel.Entity{
		IP: net.ParseIP("1.1.1.1"),
	}).Init(), Permitted)
	testEndpointMatch(t, ep, (&intel.Entity{
		IP: net.ParseIP("8.8.8.8"),
	}).Init(), NoMatch)

	// Country

	ep, err = parseEndpoint("+ AT")
	if err != nil {
		t.Fatal(err)
	}

	testEndpointMatch(t, ep, (&intel.Entity{
		IP: net.ParseIP("194.232.104.1"), // orf.at
	}).Init(), Permitted)
	testEndpointMatch(t, ep, (&intel.Entity{
		IP: net.ParseIP("151.101.1.164"), // nytimes.com
	}).Init(), NoMatch)

	// Scope

	ep, err = parseEndpoint("+ Localhost,LAN")
	if err != nil {
		t.Fatal(err)
	}

	testEndpointMatch(t, ep, (&intel.Entity{
		IP: net.ParseIP("192.168.0.1"),
	}).Init(), Permitted)
	testEndpointMatch(t, ep, (&intel.Entity{
		IP: net.ParseIP("151.101.1.164"), // nytimes.com
	}).Init(), NoMatch)

	// Lists

	_, err = parseEndpoint("+ L:A,B,C")
	if err != nil {
		t.Fatal(err)
	}
	// TODO: write test for lists matcher

}

func getLineNumberOfCaller(levels int) int {
	_, _, line, _ := runtime.Caller(levels + 1) //nolint:dogsled
	return line
}
