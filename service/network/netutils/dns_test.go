package netutils

import "testing"

func testDomainValidity(t *testing.T, domain string, isValid bool) {
	t.Helper()

	if IsValidFqdn(domain) != isValid {
		t.Errorf("domain %s failed check: was valid=%v, expected valid=%v", domain, IsValidFqdn(domain), isValid)
	}
}

func TestDNSValidation(t *testing.T) {
	t.Parallel()

	// valid
	testDomainValidity(t, ".", true)
	testDomainValidity(t, "at.", true)
	testDomainValidity(t, "orf.at.", true)
	testDomainValidity(t, "www.orf.at.", true)
	testDomainValidity(t, "a.b.c.d.e.f.g.h.i.j.k.l.m.n.o.p.q.r.s.t.u.v.x.y.z.example.org.", true)
	testDomainValidity(t, "a_a.com.", true)
	testDomainValidity(t, "a-a.com.", true)
	testDomainValidity(t, "a_a.com.", true)
	testDomainValidity(t, "a-a.com.", true)
	testDomainValidity(t, "xn--a.com.", true)
	testDomainValidity(t, "xn--asdfasdfasdfasdfasdfasdfasdfasdfasdfasdfasdfasdfasdfasdfasd.com.", true)

	// maybe valid
	testDomainValidity(t, "-.com.", true)
	testDomainValidity(t, "_.com.", true)
	testDomainValidity(t, "a_.com.", true)
	testDomainValidity(t, "a-.com.", true)
	testDomainValidity(t, "_a.com.", true)
	testDomainValidity(t, "-a.com.", true)

	// invalid
	testDomainValidity(t, ".com.", false)
	testDomainValidity(t, ".com.", false)
	testDomainValidity(t, "xn--asdfasdfasdfasdfasdfasdfasdfasdfasdfasdfasdfasdfasdfasdfasdf.com.", false)
	testDomainValidity(t, "asdfasdfasdfasdfasdfasdfasdfasdfasdfasdfasdfasdfasdfasdfasdfasdf.com.", false)
	testDomainValidity(t, "asdfasdfasdfasdfasdfasdfasdfasdfasdfasdfasdfasdfasdfasdfasdfasdf.com.", false)
	testDomainValidity(t, "asdf.asdf.asdf.asdf.asdf.asdf.asdf.asdf.asdf.asdf.asdf.asdf.asdf.asdf.asdf.asdf.asdf.asdf.asdf.asdf.asdf.asdf.asdf.asdf.asdf.asdf.asdf.asdf.asdf.asdf.asdf.asdf.asdf.asdf.asdf.asdf.asdf.asdf.asdf.asdf.asdf.asdf.asdf.asdf.asdf.asdf.asdf.asdf.asdf.asdf.as.com.", false)

	// real world examples
	testDomainValidity(t, "iuqerfsodp9ifjaposdfjhgosurijfaewrwergwea.com.", true)
}
