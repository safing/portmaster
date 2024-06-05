package resolver

import (
	"context"
	"testing"

	"github.com/safing/portmaster/base/log"
)

func testReverse(t *testing.T, ip, result, expectedErr string) {
	t.Helper()

	ctx, tracer := log.AddTracer(context.Background())
	defer tracer.Submit()

	domain, err := ResolveIPAndValidate(ctx, ip)
	if err != nil {
		tracer.Warning(err.Error())
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
	t.Parallel()

	testReverse(t, "198.41.0.4", "a.root-servers.net.", "")
	// testReverse(t, "9.9.9.9", "dns.quad9.net.", "") // started resolving to dns9.quad9.net.
	// testReverse(t, "2620:fe::fe", "dns.quad9.net.", "") // fails sometimes for some (external) reason
	testReverse(t, "1.1.1.1", "one.one.one.one.", "")
	testReverse(t, "2606:4700:4700::1111", "one.one.one.one.", "")

	testReverse(t, "93.184.216.34", "example.com.", "record could not be found: 34.216.184.93.in-addr.arpa.PTR")
	testReverse(t, "185.199.109.153", "cdn-185-199-109-153.github.com.", "record could not be found: 153.109.199.185.in-addr.arpa.PTR")
}
