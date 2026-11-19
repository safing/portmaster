package endpoints

import "testing"

func TestParsePortRange(t *testing.T) {
	t.Parallel()

	for _, rule := range []string{"+ * TCP/80-8000", "- * UDP/HTTP-HTTPS", "- * TCP/443-443"} {
		_, err := parseEndpoint(rule)
		if err != nil {
			t.Errorf("%q: unexpected error: %s", rule, err)
		}
	}
	// A reversed range never matched any port, so a block rule silently did nothing.
	for _, rule := range []string{"- * TCP/8000-80", "+ * TCP/443-80", "- * UDP/HTTPS-HTTP"} {
		_, err := parseEndpoint(rule)
		if err == nil {
			t.Errorf("%q: reversed port range was accepted", rule)
		}
	}
}
