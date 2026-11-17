package firewall

import (
	"context"
	"net"
	"reflect"
	"sync"
	"sync/atomic"
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
	runDecideBench(b, nil, nil, network.VerdictAccept)
}

// benchFilterListIDs are the source IDs of the filter lists that are active in
// the profile of BenchmarkDecideOnConnectionWithLists. The default categories
// resolve to about this many sources.
var benchFilterListIDs = []string{
	"TRAC-1", "TRAC-2", "TRAC-3", "TRAC-4", "TRAC-5",
	"MAL-1", "MAL-2", "MAL-3",
	"BAD-1", "BAD-2",
	"UNBREAK-1", "UNBREAK-2",
}

// BenchmarkDecideOnConnectionWithLists is BenchmarkDecideOnConnection with
// filter lists active in the profile, so that the filter list decider matches
// the list occurrences of the entity against them.
//
// The filter list lookup itself is not set up: it needs the bloom filter and
// the cache database of the filterlists module. The entity looks up its
// domains like with lists that are not loaded yet, and the list occurrences
// that the lookup would have found are set on the entity before the decision.
func BenchmarkDecideOnConnectionWithLists(b *testing.B) {
	b.Run("unlisted", func(b *testing.B) {
		// The entity is in no filter list, like almost all entities.
		runDecideBench(b, benchFilterListIDs, nil, network.VerdictAccept)
	})
	b.Run("inactive", func(b *testing.B) {
		// The entity is only in filter lists that are not active.
		runDecideBench(b, benchFilterListIDs, map[string][]string{
			"example.com.": {"SOC-1", "SOC-2"},
			"15133":        {"ASN-1"},
		}, network.VerdictAccept)
	})
	b.Run("listed", func(b *testing.B) {
		// A parent domain of the entity and its CNAME are in active filter lists.
		runDecideBench(b, benchFilterListIDs, map[string][]string{
			"example.com.":    {"MAL-2", "SOC-1", "TRAC-3"},
			"e1.example.net.": {"CDN-1", "TRAC-4"},
			"15133":           {"ASN-1"},
		}, network.VerdictBlock)
	})
}

// runDecideBench runs the decision benchmark with the given active filter
// lists in the profile and the given list occurrences of the entity.
func runDecideBench(b *testing.B, filterListIDs []string, listOccurrences map[string][]string, wantVerdict network.Verdict) {
	b.Helper()

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
	if filterListIDs != nil {
		// Resolving the filter list IDs of the config needs the list index of
		// the filterlists module: Set the resolved IDs directly.
		profileType := reflect.TypeFor[profile.Profile]()
		*fieldAt[[]string](unsafe.Pointer(localProfile), unexportedFieldOffset(b, profileType, "filterListIDs")) = filterListIDs //nolint:gosec
		*fieldAt[bool](unsafe.Pointer(localProfile), unexportedFieldOffset(b, profileType, "filterListsSet")) = true             //nolint:gosec
	}
	layeredProfile := profile.NewLayeredProfile(localProfile)
	// A new layered profile needs an update for the global config, which
	// would reset the list occurrences of the first entity.
	layeredProfile.Update(nil, nil)
	proc := &process.Process{Pid: 1234, Path: "/usr/lib/firefox/firefox"}
	fieldAt[atomic.Pointer[profile.LayeredProfile]](
		unsafe.Pointer(proc), //nolint:gosec // Test-only access to an unexported field.
		unexportedFieldOffset(b, reflect.TypeFor[process.Process](), "profile"),
	).Store(layeredProfile)

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
				// Set the list occurrences the filter list lookup would find.
				ListOccurences: listOccurrences,
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
		if verdict != wantVerdict {
			b.Fatalf("unexpected verdict %s: %+v", verdict, conn.Reason)
		}
	}
}
