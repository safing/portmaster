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
		Domains: map[string]*DomainDecision{
			"example.com": &DomainDecision{
				Permit:            true,
				Created:           time.Now().Unix(),
				IncludeSubdomains: false,
			},
			"bad.example.com": &DomainDecision{
				Permit:            false,
				Created:           time.Now().Unix(),
				IncludeSubdomains: true,
			},
		},
		Ports: map[int16][]*Port{
			6: []*Port{
				&Port{
					Permit:  true,
					Created: time.Now().Unix(),
					Start:   22000,
					End:     22000,
				},
			},
		},
	}

	testStampProfile = &Profile{
		ID:            "unit-test-stamp",
		Name:          "Unit Test Stamp Profile",
		SecurityLevel: status.SecurityLevelFortress,
		Flags: map[uint8]uint8{
			Internet: status.SecurityLevelsAll,
		},
		Domains: map[string]*DomainDecision{
			"bad2.example.com": &DomainDecision{
				Permit:            false,
				Created:           time.Now().Unix(),
				IncludeSubdomains: true,
			},
			"good.bad.example.com": &DomainDecision{
				Permit:            true,
				Created:           time.Now().Unix(),
				IncludeSubdomains: false,
			},
		},
		Ports: map[int16][]*Port{
			6: []*Port{
				&Port{
					Permit:  false,
					Created: time.Now().Unix(),
					Start:   80,
					End:     80,
				},
			},
			-17: []*Port{
				&Port{
					Permit:  true,
					Created: time.Now().Unix(),
					Start:   12345,
					End:     12347,
				},
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

func testDomain(t *testing.T, set *Set, domain string, shouldBePermitted bool) {
	permitted, ok := set.CheckDomain(domain)
	if !ok {
		t.Errorf("domain %s should be in test profile set", domain)
	}
	if permitted != shouldBePermitted {
		t.Errorf("unexpected result: domain %s: permitted=%v, expected=%v", domain, permitted, shouldBePermitted)
	}
}

func testUnregulatedDomain(t *testing.T, set *Set, domain string) {
	_, ok := set.CheckDomain(domain)
	if ok {
		t.Errorf("domain %s should not be in test profile set", domain)
	}
}

func testPort(t *testing.T, set *Set, listen bool, protocol uint8, port uint16, shouldBePermitted bool) {
	permitted, ok := set.CheckPort(listen, protocol, port)
	if !ok {
		t.Errorf("port [%v %d %d] should be in test profile set", listen, protocol, port)
	}
	if permitted != shouldBePermitted {
		t.Errorf("unexpected result: port [%v %d %d]: permitted=%v, expected=%v", listen, protocol, port, permitted, shouldBePermitted)
	}
}

func testUnregulatedPort(t *testing.T, set *Set, listen bool, protocol uint8, port uint16) {
	_, ok := set.CheckPort(listen, protocol, port)
	if ok {
		t.Errorf("port [%v %d %d] should not be in test profile set", listen, protocol, port)
	}
}

func TestProfileSet(t *testing.T) {

	set := NewSet(testUserProfile, testStampProfile)

	set.Update(status.SecurityLevelDynamic)
	testFlag(t, set, Whitelist, false)
	testFlag(t, set, Internet, true)
	testDomain(t, set, "example.com", true)
	testDomain(t, set, "bad.example.com", false)
	testDomain(t, set, "other.bad.example.com", false)
	testDomain(t, set, "good.bad.example.com", false)
	testDomain(t, set, "bad2.example.com", false)
	testPort(t, set, false, 6, 443, true)
	testPort(t, set, false, 6, 143, true)
	testPort(t, set, false, 6, 22, true)
	testPort(t, set, false, 6, 80, false)
	testPort(t, set, false, 6, 80, false)
	testPort(t, set, true, 17, 12345, true)
	testPort(t, set, true, 17, 12346, true)
	testPort(t, set, true, 17, 12347, true)
	testUnregulatedDomain(t, set, "other.example.com")
	testUnregulatedPort(t, set, false, 17, 53)
	testUnregulatedPort(t, set, false, 17, 443)
	testUnregulatedPort(t, set, true, 6, 443)

	set.Update(status.SecurityLevelSecure)
	testFlag(t, set, Internet, true)

	set.Update(status.SecurityLevelFortress) // Independent!
	testFlag(t, set, Internet, false)
	testPort(t, set, false, 6, 80, true)
	testUnregulatedDomain(t, set, "bad2.example.com")
	testUnregulatedPort(t, set, true, 17, 12346)
}
