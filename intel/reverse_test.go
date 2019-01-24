package intel

import "testing"

func testReverse(t *testing.T, ip, result, expectedErr string) {
	domain, err := ResolveIPAndValidate(ip, 0)
	if err != nil {
		if expectedErr == "" || err.Error() != expectedErr {
			t.Errorf("reverse-validating %s: unexpected error: %s", ip, err)
		}
		return
	}

	if domain != result {
		t.Errorf("reverse-validating %s: unexpected result: %s", ip, domain)
	}
}

func TestResolveIPAndValidate(t *testing.T) {
	testReverse(t, "198.41.0.4", "a.root-servers.net.", "")
	testReverse(t, "9.9.9.9", "dns.quad9.net.", "")
	testReverse(t, "2620:fe::fe", "dns.quad9.net.", "")
	testReverse(t, "1.1.1.1", "one.one.one.one.", "")
	testReverse(t, "2606:4700:4700::1111", "one.one.one.one.", "")

	testReverse(t, "93.184.216.34", "example.com.", "no PTR record for IP (nxDomain)")
	testReverse(t, "185.199.109.153", "sites.github.io.", "no PTR record for IP (nxDomain)")
}
