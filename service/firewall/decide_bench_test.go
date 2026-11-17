package firewall

import (
	"context"
	"net"
	"reflect"
	"sync"
	"testing"
	"unsafe"

	"github.com/tevino/abool"

	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/intel"
	"github.com/safing/portmaster/service/network"
	"github.com/safing/portmaster/service/network/netutils"
	"github.com/safing/portmaster/service/network/packet"
	"github.com/safing/portmaster/service/process"
	"github.com/safing/portmaster/service/profile"
	"github.com/safing/portmaster/service/resolver"
)

type benchProfileInstance struct {
	dataDir string
}

func (i benchProfileInstance) DataDir() string        { return i.dataDir }
func (i benchProfileInstance) Config() *config.Config { return nil }

// benchInstance provides the modules the deciders use.
type benchInstance struct {
	instance

	resolver *resolver.ResolverModule
}

func (i benchInstance) Resolver() *resolver.ResolverModule { return i.resolver }

// unexportedFieldOffset returns the offset of an unexported field of a struct
// from another package. Complete connections and processes with a profile can
// otherwise only be created by the packet path with all modules running.
func unexportedFieldOffset(tb testing.TB, structType reflect.Type, name string) uintptr {
	tb.Helper()

	field, ok := structType.FieldByName(name)
	if !ok {
		tb.Fatalf("field %s not found", name)
	}
	return field.Offset
}

// fieldAt returns a pointer to the field of type T at the given offset.
func fieldAt[T any](structPtr unsafe.Pointer, offset uintptr) *T {
	return (*T)(unsafe.Add(structPtr, offset)) //nolint:gosec // Test-only access to an unexported field.
}

var (
	benchSetup    sync.Once
	errBenchSetup error
)

// BenchmarkDecideOnConnection measures the firewall decision for the first
// packet of an outbound connection of a process with a profile that has
// endpoint rules, none of which match: all deciders run and the default
// action is applied.
func BenchmarkDecideOnConnection(b *testing.B) {
	// The profile module registers the profile config options, which may only
	// be done once. Start logging, as log lines are otherwise kept in waiting
	// goroutines, and only log critical errors.
	benchSetup.Do(func() {
		_, errBenchSetup = profile.NewModule(benchProfileInstance{dataDir: b.TempDir()})
		if errBenchSetup == nil {
			errBenchSetup = registerConfig()
		}
		if errBenchSetup == nil {
			errBenchSetup = log.Start("critical", true, "")
		}
	})
	if errBenchSetup != nil {
		b.Fatal(errBenchSetup)
	}
	prevModule := module
	module = &Firewall{instance: benchInstance{resolver: &resolver.ResolverModule{}}}
	b.Cleanup(func() { module = prevModule })

	profileConfig := map[string]any{
		profile.CfgOptionDefaultActionKey: profile.DefaultActionPermitValue,
		profile.CfgOptionEndpointsKey: []any{
			"- *.ads.example.org",
			"- tracker.example.net",
			"- AS64496",
			"- 203.0.113.0/24",
			"+ *.example.com TCP/80",
			"- 198.51.100.7",
			"- * */9999",
		},
	}
	// profile.New only logs an invalid config.
	_, err := config.NewPerspective(config.Expand(profileConfig))
	if err != nil {
		b.Fatal(err)
	}
	localProfile := profile.New(&profile.Profile{
		ID:     "bench",
		Source: profile.SourceLocal,
		Name:   "Bench",
		Config: profileConfig,
	})
	proc := &process.Process{Pid: 1234, Path: "/usr/lib/firefox/firefox"}
	*fieldAt[*profile.LayeredProfile](
		unsafe.Pointer(proc), //nolint:gosec // Test-only access to an unexported field.
		unexportedFieldOffset(b, reflect.TypeFor[process.Process](), "profile"),
	) = profile.NewLayeredProfile(localProfile)

	connProcess := unexportedFieldOffset(b, reflect.TypeFor[network.Connection](), "process")
	connDataComplete := unexportedFieldOffset(b, reflect.TypeFor[network.Connection](), "dataComplete")
	entityLocationOnce := unexportedFieldOffset(b, reflect.TypeFor[intel.Entity](), "fetchLocationOnce")

	pkt := newVerdictTestPacket(packet.TCP, false)
	ctx := context.Background()
	dataComplete := abool.NewBool(true)

	b.ReportAllocs()
	for b.Loop() {
		conn := &network.Connection{
			ID:         "bench",
			Type:       network.IPConnection,
			IPVersion:  packet.IPv4,
			IPProtocol: packet.TCP,
			LocalIP:    net.IPv4(192, 168, 1, 10),
			LocalPort:  40000,
			PID:        1234,
			Entity: (&intel.Entity{
				IP:       net.IPv4(93, 184, 216, 34),
				IPScope:  netutils.Global,
				Protocol: 6,
				Port:     443,
				Domain:   "www.example.com.",
				CNAME:    []string{"e1.example.net."},
				Country:  "US",
				ASN:      15133,
				ASOrg:    "Edgecast Inc.",
			}).Init(443),
		}
		// The location was already looked up when the entity was created; the
		// geoip database is not available here.
		fieldAt[sync.Once](unsafe.Pointer(conn.Entity), entityLocationOnce).Do(func() {})  //nolint:gosec
		*fieldAt[*process.Process](unsafe.Pointer(conn), connProcess) = proc               //nolint:gosec
		*fieldAt[*abool.AtomicBool](unsafe.Pointer(conn), connDataComplete) = dataComplete //nolint:gosec

		conn.Lock()
		decideOnConnection(ctx, conn, pkt)
		verdict := conn.Verdict
		conn.Unlock()
		if verdict != network.VerdictAccept {
			b.Fatalf("unexpected verdict %s: %+v", verdict, conn.Reason)
		}
	}
}
