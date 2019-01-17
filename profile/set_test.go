package profile

import (
	"testing"
	"time"

	"github.com/Safing/portmaster/status"
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
				DomainOrIP: "good.bad.example.com.",
				Wildcard:   false,
				Permit:     true,
				Created:    time.Now().Unix(),
			},
			&EndpointPermission{
				DomainOrIP: "bad.example.com.",
				Wildcard:   true,
				Permit:     false,
				Created:    time.Now().Unix(),
			},
			&EndpointPermission{
				DomainOrIP: "example.com.",
				Wildcard:   false,
				Permit:     true,
				Created:    time.Now().Unix(),
			},
			&EndpointPermission{
				DomainOrIP: "",
				Wildcard:   true,
				Permit:     true,
				Protocol:   6,
				StartPort:  22000,
				EndPort:    22000,
				Created:    time.Now().Unix(),
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
				DomainOrIP: "bad2.example.com.",
				Wildcard:   true,
				Permit:     false,
				Created:    time.Now().Unix(),
			},
			&EndpointPermission{
				DomainOrIP: "",
				Wildcard:   true,
				Permit:     true,
				Protocol:   6,
				StartPort:  80,
				EndPort:    80,
				Created:    time.Now().Unix(),
			},
		},
		ServiceEndpoints: []*EndpointPermission{
			&EndpointPermission{
				DomainOrIP: "",
				Wildcard:   true,
				Permit:     true,
				Protocol:   17,
				StartPort:  12345,
				EndPort:    12347,
				Created:    time.Now().Unix(),
			},
			&EndpointPermission{ // default deny
				DomainOrIP: "",
				Wildcard:   true,
				Permit:     false,
				Created:    time.Now().Unix(),
			},
		},
	}
}

func testFlag(t *testing.T, set *Set, flag uint8, shouldBeActive bool) {
	active := set.CheckFlag(flag)
	if active != shouldBeActive {
		t.Errorf("unexpected result: flag %s: permitted=%v, expected=%v", flagNames[flag], active, shouldBeActive)
	}
}

func testEndpoint(t *testing.T, set *Set, domainOrIP string, protocol uint8, port uint16, inbound bool, shouldBePermitted bool) {
	var permitted, ok bool
	permitted, _, ok = set.CheckEndpoint(domainOrIP, protocol, port, inbound)
	if !ok {
		t.Errorf("endpoint %s/%d/%d/%v should be in test profile set", domainOrIP, protocol, port, inbound)
	}
	if permitted != shouldBePermitted {
		t.Errorf("unexpected result for endpoint %s/%d/%d/%v: permitted=%v, expected=%v", domainOrIP, protocol, port, inbound, permitted, shouldBePermitted)
	}
}

func testUnregulatedEndpoint(t *testing.T, set *Set, domainOrIP string, protocol uint8, port uint16, inbound bool) {
	_, _, ok := set.CheckEndpoint(domainOrIP, protocol, port, inbound)
	if ok {
		t.Errorf("endpoint %s/%d/%d/%v should not be in test profile set", domainOrIP, protocol, port, inbound)
	}
}

func TestProfileSet(t *testing.T) {

	set := NewSet(testUserProfile, testStampProfile)

	set.Update(status.SecurityLevelDynamic)
	testFlag(t, set, Whitelist, false)
	// testFlag(t, set, Internet, true)
	testEndpoint(t, set, "example.com.", 0, 0, false, true)
	testEndpoint(t, set, "bad.example.com.", 0, 0, false, false)
	testEndpoint(t, set, "other.bad.example.com.", 0, 0, false, false)
	testEndpoint(t, set, "good.bad.example.com.", 0, 0, false, true)
	testEndpoint(t, set, "bad2.example.com.", 0, 0, false, false)
	testEndpoint(t, set, "10.2.3.4", 6, 22000, false, true)
	testEndpoint(t, set, "fd00::1", 6, 22000, false, true)
	testEndpoint(t, set, "test.local.", 6, 22000, false, true)
	testUnregulatedEndpoint(t, set, "other.example.com.", 0, 0, false)
	testUnregulatedEndpoint(t, set, "10.2.3.4", 17, 53, false)
	testUnregulatedEndpoint(t, set, "10.2.3.4", 17, 443, false)
	testUnregulatedEndpoint(t, set, "10.2.3.4", 6, 12346, false)
	testEndpoint(t, set, "10.2.3.4", 17, 12345, true, true)
	testEndpoint(t, set, "fd00::1", 17, 12347, true, true)

	set.Update(status.SecurityLevelSecure)
	// testFlag(t, set, Internet, true)

	set.Update(status.SecurityLevelFortress) // Independent!
	testFlag(t, set, Whitelist, true)
	testEndpoint(t, set, "10.2.3.4", 17, 12345, true, false)
	testEndpoint(t, set, "fd00::1", 17, 12347, true, false)
	testUnregulatedEndpoint(t, set, "10.2.3.4", 6, 80, false)
	testUnregulatedEndpoint(t, set, "bad2.example.com.", 0, 0, false)
}
