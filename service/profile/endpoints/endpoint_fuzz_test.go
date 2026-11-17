package endpoints

import (
	"context"
	"net"
	"testing"

	"github.com/safing/portmaster/service/intel"
)

var fuzzSeeds = []string{
	"- *", "+ *", "- *bad*", "- bad*", "- *bad.com", "- .bad.com", "- bad.com",
	"- www.bad.com.", "+ 127.0.0.1", "+ ::1", "+ 2606:4700:4700::1111",
	"+ 127.0.0.0/8", "+ 2606:4700:4700::/48", "+ ::ffff:1.2.3.0/120", "+ ::ffff:1.2.3.4",
	"+ DE", "+ AS", "+ C:EU", "+ AS1234", "+ Localhost", "+ LAN", "+ Internet",
	"+ Localhost,LAN,Internet", "+ LAN,LAN", "+ * TCP/1-1024", "+ * */DNS", "+ * ICMP",
	"+ * 127", "+ * UDP/1234", "+ * TCP/HTTP", "+ * TCP/80-443", "+ * TCP/443-80",
	"+ * TCP/20", "+ * TCP/FTP-DATA", "+ * 6/65535", "+ * 6/65536", "+ * 256",
	"+ L:MAL", "- L:MAL,TRAC", "- L:", "+ example.* */HTTPS", "+ .example.com",
	"+ *example.com", "+ example.com # comment", "+ #", "+ # comment", "# foo bar",
	"+", "-", "", " ", "+ .", "+ *.", "+ **", "+ ä.com", "+ 1.2.3.4 UDP-LITE/1-2",
}

var fuzzEntities = []*intel.Entity{
	(&intel.Entity{}).Init(0),
	(&intel.Entity{Domain: apexDomain}).Init(0),
	(&intel.Entity{IP: net.ParseIP("127.0.0.1"), Protocol: 6, Port: 80}).Init(0),
	(&intel.Entity{IP: net.ParseIP("192.168.1.1"), Domain: "bad.com.", Protocol: 17, Port: 53}).Init(0),
	(&intel.Entity{IP: net.ParseIP("::ffff:1.2.3.4"), Protocol: 1}).Init(0),
}

func FuzzParseEndpoint(f *testing.F) {
	for _, s := range fuzzSeeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, value string) {
		ep, err := parseEndpoint(value)
		if err != nil {
			return
		}
		if ep == nil {
			t.Fatalf("nil endpoint without error for %q", value)
		}
		s := ep.String()
		ep2, err := parseEndpoint(s)
		if err != nil {
			t.Fatalf("%q parsed, but String() %q does not: %s", value, s, err)
		}
		if s2 := ep2.String(); s2 != s {
			t.Fatalf("%q: unstable String(): %q -> %q", value, s, s2)
		}
		for _, e := range fuzzEntities {
			r1, _ := ep.Matches(context.Background(), e)
			r2, _ := ep2.Matches(context.Background(), e)
			if r1 != r2 {
				t.Fatalf("%q vs re-parsed %q: match result differs for %+v: %s vs %s", value, s, e, r1, r2)
			}
		}
	})
}
