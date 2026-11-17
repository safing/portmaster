package nsutil

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/miekg/dns"

	"github.com/safing/portmaster/base/log"
)

// parseMessageRecord creates the message record with the zone parser, as
// MakeMessageRecord does for messages that makePlainMessageRecord does not
// handle.
func parseMessageRecord(level log.Severity, msg string) (dns.RR, error) {
	return dns.NewRR(fmt.Sprintf(
		`%s.portmaster. 0 IN TXT "%s"`,
		strings.ToLower(level.String()),
		msg,
	))
}

func checkPlainMessageRecord(t *testing.T, level log.Severity, msg string) {
	t.Helper()

	plain := makePlainMessageRecord(level, msg)
	if plain == nil {
		return
	}
	parsed, err := parseMessageRecord(level, msg)
	if err != nil {
		t.Fatalf("message %q: parser failed, but plain record was created: %s", msg, err)
	}
	if !reflect.DeepEqual(dns.RR(plain), parsed) {
		t.Fatalf("message %q: plain record %#v differs from parsed record %#v", msg, plain, parsed)
	}
}

func TestMakePlainMessageRecord(t *testing.T) {
	t.Parallel()

	var printable strings.Builder
	for c := byte(' '); c <= '~'; c++ {
		if c != '"' && c != '\\' {
			printable.WriteByte(c)
		}
	}

	lengths := []int{1, 254, 255, 256, 509, 510, 511, 765, 1000, 4096, 70000}
	msgs := make([]string, 0, 7+len(lengths))
	msgs = append(msgs,
		"",
		"served from cache, resolved by Quad9 (dot://9.9.9.9:853#config)",
		"record valid for 59m59s",
		"internal error: failed to check if request is local",
		"semi;colon (parens) $dollar @at",
		"  leading and trailing spaces  ",
		printable.String(),
	)
	for _, length := range lengths {
		msgs = append(msgs, strings.Repeat(printable.String(), length/printable.Len()+1)[:length])
	}

	levels := []log.Severity{
		log.TraceLevel, log.DebugLevel, log.InfoLevel, log.WarningLevel,
		log.ErrorLevel, log.CriticalLevel, log.Severity(255),
	}
	for _, level := range levels {
		for _, msg := range msgs {
			if makePlainMessageRecord(level, msg) == nil {
				t.Fatalf("message %q at level %s: expected plain record", msg, level)
			}
			checkPlainMessageRecord(t, level, msg)
		}
	}

	// Messages the parser changes or rejects must use the parser.
	for _, msg := range []string{`quote " in message`, `back\slash`, `\065`, "new\nline", "tab\tchar", "umlaut ä", "nul\x00"} {
		if makePlainMessageRecord(log.InfoLevel, msg) != nil {
			t.Fatalf("message %q: expected no plain record", msg)
		}
	}
}

func FuzzMakePlainMessageRecord(f *testing.F) {
	for _, seed := range []string{"", "message", `quote "seed"`, strings.Repeat("x", 300)} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, msg string) {
		checkPlainMessageRecord(t, log.InfoLevel, msg)
	})
}

func BenchmarkMakeMessageRecord(b *testing.B) {
	msg := "served from cache, resolved by Quad9 (dot://9.9.9.9:853#config)"

	b.ReportAllocs()
	for b.Loop() {
		_, err := MakeMessageRecord(log.InfoLevel, msg)
		if err != nil {
			b.Fatal(err)
		}
	}
}
