package intel

import "testing"

func testDomains(t *testing.T, ipi *IPInfo, expectedDomains string) {
	if ipi.FmtDomains() != expectedDomains {
		t.Errorf("unexpected domains '%s', expected '%s'", ipi.FmtDomains(), expectedDomains)
	}
}

func TestIPInfo(t *testing.T) {
	ipi := &IPInfo{
		IP:      "1.2.3.4",
		Domains: []string{"example.com.", "sub.example.com."},
	}

	testDomains(t, ipi, "example.com. or sub.example.com.")
	ipi.AddDomain("added.example.com.")
	testDomains(t, ipi, "added.example.com. or example.com. or sub.example.com.")
	ipi.AddDomain("sub.example.com.")
	testDomains(t, ipi, "added.example.com. or example.com. or sub.example.com.")
	ipi.AddDomain("added.example.com.")
	testDomains(t, ipi, "added.example.com. or example.com. or sub.example.com.")

}
