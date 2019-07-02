package profile

import (
	"net"
	"testing"
	"time"

	"github.com/safing/portbase/utils/testutils"
	"github.com/safing/portmaster/status"
)

var (
	testUserProfile  *Profile
	testStampProfile *Profile
)

func init() {
	specialProfileLock.Lock()
	defer specialProfileLock.Unlock()

	globalProfile = makeDefaultGlobalProfile()
	fallbackProfile = makeDefaultFallbackProfile()

	testUserProfile = &Profile{
		ID:            "unit-test-user",
		Name:          "Unit Test User Profile",
		SecurityLevel: status.SecurityLevelDynamic,
		Flags: map[uint8]uint8{
			Independent: status.SecurityLevelFortress,
		},
		Endpoints: []*EndpointPermission{
			&EndpointPermission{
				Type:    EptDomain,
				Value:   "good.bad.example.com.",
				Permit:  true,
				Created: time.Now().Unix(),
			},
			&EndpointPermission{
				Type:    EptDomain,
				Value:   "*bad.example.com.",
				Permit:  false,
				Created: time.Now().Unix(),
			},
			&EndpointPermission{
				Type:    EptDomain,
				Value:   "example.com.",
				Permit:  true,
				Created: time.Now().Unix(),
			},
			&EndpointPermission{
				Type:      EptAny,
				Permit:    true,
				Protocol:  6,
				StartPort: 22000,
				EndPort:   22000,
				Created:   time.Now().Unix(),
			},
		},
	}

	testStampProfile = &Profile{
		ID:            "unit-test-stamp",
		Name:          "Unit Test Stamp Profile",
		SecurityLevel: status.SecurityLevelFortress,
		// Flags: map[uint8]uint8{
		// 	Internet: status.SecurityLevelsAll,
		// },
		Endpoints: []*EndpointPermission{
			&EndpointPermission{
				Type:    EptDomain,
				Value:   "*bad2.example.com.",
				Permit:  false,
				Created: time.Now().Unix(),
			},
			&EndpointPermission{
				Type:      EptAny,
				Permit:    true,
				Protocol:  6,
				StartPort: 80,
				EndPort:   80,
				Created:   time.Now().Unix(),
			},
		},
		ServiceEndpoints: []*EndpointPermission{
			&EndpointPermission{
				Type:      EptAny,
				Permit:    true,
				Protocol:  17,
				StartPort: 12345,
				EndPort:   12347,
				Created:   time.Now().Unix(),
			},
			&EndpointPermission{ // default deny
				Type:    EptAny,
				Permit:  false,
				Created: time.Now().Unix(),
			},
		},
	}
}

func testFlag(t *testing.T, set *Set, flag uint8, shouldBeActive bool) {
	active := set.CheckFlag(flag)
	if active != shouldBeActive {
		t.Errorf("unexpected result: flag %s: active=%v, expected=%v", flagNames[flag], active, shouldBeActive)
	}
}

func testEndpointDomain(t *testing.T, set *Set, domain string, expectedResult EPResult) {
	var result EPResult
	result, _ = set.CheckEndpointDomain(domain)
	if result != expectedResult {
		t.Errorf(
			"line %d: unexpected result for endpoint domain %s: result=%s, expected=%s",
			testutils.GetLineNumberOfCaller(1),
			domain,
			result,
			expectedResult,
		)
	}
}

func testEndpointIP(t *testing.T, set *Set, domain string, ip net.IP, protocol uint8, port uint16, inbound bool, expectedResult EPResult) {
	var result EPResult
	result, _ = set.CheckEndpointIP(domain, ip, protocol, port, inbound)
	if result != expectedResult {
		t.Errorf(
			"line %d: unexpected result for endpoint %s/%s/%d/%d/%v: result=%s, expected=%s",
			testutils.GetLineNumberOfCaller(1),
			domain,
			ip,
			protocol,
			port,
			inbound,
			result,
			expectedResult,
		)
	}
}

func TestProfileSet(t *testing.T) {

	set := NewSet("[pid]-/path/to/bin", testUserProfile, testStampProfile)

	set.Update(status.SecurityLevelDynamic)
	testFlag(t, set, Whitelist, false)
	// testFlag(t, set, Internet, true)
	testEndpointDomain(t, set, "example.com.", Permitted)
	testEndpointDomain(t, set, "bad.example.com.", Denied)
	testEndpointDomain(t, set, "other.bad.example.com.", Denied)
	testEndpointDomain(t, set, "good.bad.example.com.", Permitted)
	testEndpointDomain(t, set, "bad2.example.com.", Undeterminable)
	testEndpointIP(t, set, "", net.ParseIP("10.2.3.4"), 6, 22000, false, Permitted)
	testEndpointIP(t, set, "", net.ParseIP("fd00::1"), 6, 22000, false, Permitted)
	testEndpointDomain(t, set, "test.local.", Undeterminable)
	testEndpointDomain(t, set, "other.example.com.", Undeterminable)
	testEndpointIP(t, set, "", net.ParseIP("10.2.3.4"), 17, 53, false, NoMatch)
	testEndpointIP(t, set, "", net.ParseIP("10.2.3.4"), 17, 443, false, NoMatch)
	testEndpointIP(t, set, "", net.ParseIP("10.2.3.4"), 6, 12346, false, NoMatch)
	testEndpointIP(t, set, "", net.ParseIP("10.2.3.4"), 17, 12345, true, Permitted)
	testEndpointIP(t, set, "", net.ParseIP("fd00::1"), 17, 12347, true, Permitted)

	set.Update(status.SecurityLevelSecure)
	// testFlag(t, set, Internet, true)

	set.Update(status.SecurityLevelFortress) // Independent!
	testFlag(t, set, Whitelist, true)
	testEndpointIP(t, set, "", net.ParseIP("10.2.3.4"), 17, 12345, true, Denied)
	testEndpointIP(t, set, "", net.ParseIP("fd00::1"), 17, 12347, true, Denied)
	testEndpointIP(t, set, "", net.ParseIP("10.2.3.4"), 6, 80, false, NoMatch)
	testEndpointDomain(t, set, "bad2.example.com.", Undeterminable)
}
