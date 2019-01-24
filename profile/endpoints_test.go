package profile

import (
	"testing"
)

// TODO: RETIRED
// func testdeMatcher(t *testing.T, value string, expectedResult bool) {
// 	if domainEndingMatcher.MatchString(value) != expectedResult {
// 		if expectedResult {
// 			t.Errorf("domainEndingMatcher should match %s", value)
// 		} else {
// 			t.Errorf("domainEndingMatcher should not match %s", value)
// 		}
// 	}
// }
//
// func TestdomainEndingMatcher(t *testing.T) {
// 	testdeMatcher(t, "example.com", true)
// 	testdeMatcher(t, "com", true)
// 	testdeMatcher(t, "example.xn--lgbbat1ad8j", true)
// 	testdeMatcher(t, "xn--lgbbat1ad8j", true)
// 	testdeMatcher(t, "fe80::beef", false)
// 	testdeMatcher(t, "fe80::dead:beef", false)
// 	testdeMatcher(t, "10.2.3.4", false)
// 	testdeMatcher(t, "4", false)
// }

func TestEPString(t *testing.T) {
	var endpoints Endpoints
	endpoints = []*EndpointPermission{
		&EndpointPermission{
			DomainOrIP: "example.com",
			Wildcard:   false,
			Protocol:   6,
			Permit:     true,
		},
		&EndpointPermission{
			DomainOrIP: "8.8.8.8",
			Protocol:   17, // TCP
			StartPort:  53, // DNS
			EndPort:    53,
			Permit:     false,
		},
		&EndpointPermission{
			DomainOrIP: "google.com",
			Wildcard:   true,
			Permit:     false,
		},
	}
	if endpoints.String() != "[example.com 6/*, 8.8.8.8 17/53, google.com */*]" {
		t.Errorf("unexpected result: %s", endpoints.String())
	}

	var noEndpoints Endpoints
	noEndpoints = []*EndpointPermission{}
	if noEndpoints.String() != "[]" {
		t.Errorf("unexpected result: %s", noEndpoints.String())
	}

}
