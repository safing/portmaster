package resolver

import "testing"

func TestCheckResolverSearchScope(t *testing.T) {
	t.Parallel()

	test := func(t *testing.T, domain string, expectedResult bool) {
		t.Helper()

		if checkSearchScope(domain) != expectedResult {
			if expectedResult {
				t.Errorf("domain %s failed scope test", domain)
			} else {
				t.Errorf("domain %s should fail scope test", domain)
			}
		}
	}

	// should fail (invalid)
	test(t, ".", false)
	test(t, ".com.", false)
	test(t, "com.", false)
	test(t, ".com", false)

	// should succeed
	test(t, "a.com", true)
	test(t, "b.a.com", true)
	test(t, "c.b.a.com", true)

	test(t, "onion", true)
	test(t, "a.onion", true)
	test(t, "b.a.onion", true)
	test(t, "c.b.a.onion", true)

	test(t, "bit", true)
	test(t, "a.bit", true)
	test(t, "b.a.bit", true)
	test(t, "c.b.a.bit", true)
}
