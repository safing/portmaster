package hub

import (
	"testing"
)

func FuzzParseTransport(f *testing.F) {
	for _, seed := range []string{
		"spn:17",
		"smtp:25",
		"smtp://:25",
		"http:80",
		"http://example.com:80/example",
		"https:443",
		"wss://example.com:443/spn",
		"tcp://1.2.3.4:17#Zwu5LtU3DLjoV1KNg5Vzwns3eWStB1hjUQuCQNP5bWVQmA",
		"tcp://[2001:db8::1]:17/path?q=1#option",
		"spn:17/",
		"spn:0",
		"spn:65536",
		"spn://user:pass@example.com:17",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, definition string) {
		_, _, _, _ = ParseBootstrapHub(definition) //nolint:dogsled
		ParseTransports([]string{definition, definition})

		tr, err := ParseTransport(definition)
		if err != nil {
			return
		}
		if tr.Port == 0 || tr.Protocol == "" {
			t.Fatalf("accepted transport without port or protocol: %+v", tr)
		}
		_ = tr.String()
	})
}
