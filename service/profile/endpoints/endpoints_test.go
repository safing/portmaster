package endpoints

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/safing/portmaster/base/api"
	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/base/database/dbmodule"
	"github.com/safing/portmaster/base/notifications"
	"github.com/safing/portmaster/service/configure"
	"github.com/safing/portmaster/service/intel"
	"github.com/safing/portmaster/service/intel/geoip"
	"github.com/safing/portmaster/service/ui"
	"github.com/safing/portmaster/service/updates"
)

type testInstance struct {
	db           *dbmodule.DBModule
	api          *api.API
	config       *config.Config
	intelUpdates *updates.Updater
	geoip        *geoip.GeoIP
}

func (stub *testInstance) IntelUpdates() *updates.Updater              { return stub.intelUpdates }
func (stub *testInstance) API() *api.API                               { return stub.api }
func (stub *testInstance) Config() *config.Config                      { return stub.config }
func (stub *testInstance) Notifications() *notifications.Notifications { return nil }
func (stub *testInstance) Ready() bool                                 { return true }
func (stub *testInstance) Restart()                                    {}
func (stub *testInstance) Shutdown()                                   {}
func (stub *testInstance) SetCmdLineOperation(f func() error)          {}
func (stub *testInstance) BinaryUpdates() *updates.Updater             { return nil }
func (stub *testInstance) UI() *ui.UI                                  { return nil }
func (stub *testInstance) DataDir() string                             { return _dataDir }

var _dataDir string

func runTest(m *testing.M) error {
	var err error

	// Create a temporary directory for testing
	_dataDir, err = os.MkdirTemp("", "")
	if err != nil {
		return fmt.Errorf("failed to create temporary data directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(_dataDir) }()

	// Initialize the Intel update configuration
	intelUpdateConfig := updates.Config{
		Name:              configure.DefaultIntelIndexName,
		Directory:         filepath.Join(_dataDir, "test_intel"),
		DownloadDirectory: filepath.Join(_dataDir, "test_download_intel"),
		PurgeDirectory:    filepath.Join(_dataDir, "test_upgrade_obsolete_intel"),
		IndexURLs:         configure.DefaultIntelIndexURLs,
		IndexFile:         "index.json",
		AutoCheck:         true,
		AutoDownload:      true,
		AutoApply:         true,
	}

	// Set the default API listen address
	api.SetDefaultAPIListenAddress("0.0.0.0:8080")

	// Initialize the instance with the necessary components
	stub := &testInstance{}
	stub.db, err = dbmodule.New(stub)
	if err != nil {
		return fmt.Errorf("failed to create database: %w", err)
	}
	stub.config, err = config.New(stub)
	if err != nil {
		return fmt.Errorf("failed to create config: %w", err)
	}
	stub.api, err = api.New(stub)
	if err != nil {
		return fmt.Errorf("failed to create api: %w", err)
	}
	stub.intelUpdates, err = updates.New(stub, "Intel Updater", intelUpdateConfig)
	if err != nil {
		return fmt.Errorf("failed to create updates: %w", err)
	}
	stub.geoip, err = geoip.New(stub)
	if err != nil {
		return fmt.Errorf("failed to create geoip: %w", err)
	}

	err = stub.db.Start()
	if err != nil {
		return fmt.Errorf("Failed to start database: %w", err)
	}
	err = stub.config.Start()
	if err != nil {
		return fmt.Errorf("Failed to start config: %w", err)
	}
	err = stub.api.Start()
	if err != nil {
		return fmt.Errorf("Failed to start api: %w", err)
	}
	err = stub.intelUpdates.Start()
	if err != nil {
		return fmt.Errorf("Failed to start updates: %w", err)
	}
	err = stub.geoip.Start()
	if err != nil {
		return fmt.Errorf("Failed to start geoip: %w", err)
	}

	m.Run()
	return nil
}

func TestMain(m *testing.M) {
	if err := runTest(m); err != nil {
		fmt.Printf("%s", err)
		os.Exit(1)
	}
}

func testEndpointMatch(t *testing.T, ep Endpoint, entity *intel.Entity, expectedResult EPResult) {
	t.Helper()

	result, _ := ep.Matches(context.TODO(), entity)
	if result != expectedResult {
		t.Errorf(
			"line %d: unexpected result for endpoint %s and entity %+v: result=%s, expected=%s",
			getLineNumberOfCaller(1),
			ep,
			entity,
			result,
			expectedResult,
		)
	}
}

func testFormat(t *testing.T, endpoint string, shouldSucceed bool) {
	t.Helper()

	_, err := parseEndpoint(endpoint)
	if shouldSucceed {
		assert.NoError(t, err)
	} else {
		assert.Error(t, err)
	}
}

func TestEndpointFormat(t *testing.T) {
	t.Parallel()

	testFormat(t, "+ .", false)
	testFormat(t, "+ .at", true)
	testFormat(t, "+ .at.", true)
	testFormat(t, "+ 1.at", true)
	testFormat(t, "+ 1.at.", true)
	testFormat(t, "+ 1.f.ix.de.", true)
	testFormat(t, "+ *contains*", true)
	testFormat(t, "+ *has.suffix", true)
	testFormat(t, "+ *.has.suffix", true)
	testFormat(t, "+ *has.prefix*", true)
	testFormat(t, "+ *has.prefix.*", true)
	testFormat(t, "+ .sub.and.prefix.*", false)
	testFormat(t, "+ *.sub..and.prefix.*", false)
}

func TestEndpointMatching(t *testing.T) { //nolint:maintidx // TODO
	t.Parallel()

	// ANY

	ep, err := parseEndpoint("+ *")
	if err != nil {
		t.Fatal(err)
	}

	testEndpointMatch(t, ep, (&intel.Entity{
		Domain: "example.com.",
	}).Init(0), Permitted)
	testEndpointMatch(t, ep, (&intel.Entity{
		Domain:   "example.com.",
		IP:       net.ParseIP("10.2.3.4"),
		Protocol: 6,
		Port:     443,
	}).Init(0), Permitted)

	// DOMAIN

	// wildcard domains
	ep, err = parseEndpoint("+ *example.com")
	if err != nil {
		t.Fatal(err)
	}

	testEndpointMatch(t, ep, (&intel.Entity{
		Domain: "example.com.",
	}).Init(0), Permitted)
	testEndpointMatch(t, ep, (&intel.Entity{
		Domain: "abc.example.com.",
	}).Init(0), Permitted)
	testEndpointMatch(t, ep, (&intel.Entity{
		Domain: "abc-example.com.",
	}).Init(0), Permitted)
	testEndpointMatch(t, ep, (&intel.Entity{
		Domain:   "example.com.",
		IP:       net.ParseIP("10.2.3.4"),
		Protocol: 6,
		Port:     443,
	}).Init(0), Permitted)
	testEndpointMatch(t, ep, (&intel.Entity{
		Domain:   "abc.example.com.",
		IP:       net.ParseIP("10.2.3.4"),
		Protocol: 6,
		Port:     443,
	}).Init(0), Permitted)
	testEndpointMatch(t, ep, (&intel.Entity{
		Domain:   "abc-example.com.",
		IP:       net.ParseIP("10.2.3.4"),
		Protocol: 6,
		Port:     443,
	}).Init(0), Permitted)

	ep, err = parseEndpoint("+ *.example.com")
	if err != nil {
		t.Fatal(err)
	}

	testEndpointMatch(t, ep, (&intel.Entity{
		Domain: "example.com.",
	}).Init(0), NoMatch)
	testEndpointMatch(t, ep, (&intel.Entity{
		Domain: "abc.example.com.",
	}).Init(0), Permitted)
	testEndpointMatch(t, ep, (&intel.Entity{
		Domain: "abc-example.com.",
	}).Init(0), NoMatch)
	testEndpointMatch(t, ep, (&intel.Entity{
		Domain:   "example.com.",
		IP:       net.ParseIP("10.2.3.4"),
		Protocol: 6,
		Port:     443,
	}).Init(0), NoMatch)
	testEndpointMatch(t, ep, (&intel.Entity{
		Domain:   "abc.example.com.",
		IP:       net.ParseIP("10.2.3.4"),
		Protocol: 6,
		Port:     443,
	}).Init(0), Permitted)
	testEndpointMatch(t, ep, (&intel.Entity{
		Domain:   "abc-example.com.",
		IP:       net.ParseIP("10.2.3.4"),
		Protocol: 6,
		Port:     443,
	}).Init(0), NoMatch)

	ep, err = parseEndpoint("+ .example.com")
	if err != nil {
		t.Fatal(err)
	}

	testEndpointMatch(t, ep, (&intel.Entity{
		Domain: "example.com.",
	}).Init(0), Permitted)
	testEndpointMatch(t, ep, (&intel.Entity{
		Domain: "abc.example.com.",
	}).Init(0), Permitted)
	testEndpointMatch(t, ep, (&intel.Entity{
		Domain: "abc-example.com.",
	}).Init(0), NoMatch)
	testEndpointMatch(t, ep, (&intel.Entity{
		Domain:   "example.com.",
		IP:       net.ParseIP("10.2.3.4"),
		Protocol: 6,
		Port:     443,
	}).Init(0), Permitted)
	testEndpointMatch(t, ep, (&intel.Entity{
		Domain:   "abc.example.com.",
		IP:       net.ParseIP("10.2.3.4"),
		Protocol: 6,
		Port:     443,
	}).Init(0), Permitted)
	testEndpointMatch(t, ep, (&intel.Entity{
		Domain:   "abc-example.com.",
		IP:       net.ParseIP("10.2.3.4"),
		Protocol: 6,
		Port:     443,
	}).Init(0), NoMatch)

	ep, err = parseEndpoint("+ example.*")
	if err != nil {
		t.Fatal(err)
	}

	testEndpointMatch(t, ep, (&intel.Entity{
		Domain: "example.com.",
	}).Init(0), Permitted)
	testEndpointMatch(t, ep, (&intel.Entity{
		Domain: "abc.example.com.",
	}).Init(0), NoMatch)
	testEndpointMatch(t, ep, (&intel.Entity{
		Domain:   "example.com.",
		IP:       net.ParseIP("10.2.3.4"),
		Protocol: 6,
		Port:     443,
	}).Init(0), Permitted)
	testEndpointMatch(t, ep, (&intel.Entity{
		Domain:   "abc.example.com.",
		IP:       net.ParseIP("10.2.3.4"),
		Protocol: 6,
		Port:     443,
	}).Init(0), NoMatch)

	ep, err = parseEndpoint("+ *.exampl*")
	if err != nil {
		t.Fatal(err)
	}

	testEndpointMatch(t, ep, (&intel.Entity{
		Domain: "example.com.",
	}).Init(0), NoMatch)
	testEndpointMatch(t, ep, (&intel.Entity{
		Domain: "abc.example.com.",
	}).Init(0), Permitted)
	testEndpointMatch(t, ep, (&intel.Entity{
		Domain:   "example.com.",
		IP:       net.ParseIP("10.2.3.4"),
		Protocol: 6,
		Port:     443,
	}).Init(0), NoMatch)
	testEndpointMatch(t, ep, (&intel.Entity{
		Domain:   "abc.example.com.",
		IP:       net.ParseIP("10.2.3.4"),
		Protocol: 6,
		Port:     443,
	}).Init(0), Permitted)

	ep, err = parseEndpoint("+ *.com.")
	if err != nil {
		t.Fatal(err)
	}

	testEndpointMatch(t, ep, (&intel.Entity{
		Domain: "example.com.",
	}).Init(0), Permitted)
	testEndpointMatch(t, ep, (&intel.Entity{
		Domain: "example.org.",
	}).Init(0), NoMatch)

	// protocol

	ep, err = parseEndpoint("+ example.com UDP")
	if err != nil {
		t.Fatal(err)
	}

	testEndpointMatch(t, ep, (&intel.Entity{
		Domain:   "example.com.",
		IP:       net.ParseIP("10.2.3.4"),
		Protocol: 6,
		Port:     443,
	}).Init(0), NoMatch)
	testEndpointMatch(t, ep, (&intel.Entity{
		Domain:   "example.com.",
		IP:       net.ParseIP("10.2.3.4"),
		Protocol: 17,
		Port:     443,
	}).Init(0), Permitted)
	testEndpointMatch(t, ep, (&intel.Entity{
		Domain: "example.com.",
	}).Init(0), NoMatch)

	// ports

	ep, err = parseEndpoint("+ example.com 17/442-444")
	if err != nil {
		t.Fatal(err)
	}

	entity := (&intel.Entity{
		Domain:   "example.com.",
		IP:       net.ParseIP("10.2.3.4"),
		Protocol: 17,
		Port:     441,
	}).Init(0)
	testEndpointMatch(t, ep, entity, NoMatch)

	entity.Port = 442
	entity.Init(0)
	testEndpointMatch(t, ep, entity, Permitted)

	entity.Port = 443
	entity.Init(0)
	testEndpointMatch(t, ep, entity, Permitted)

	entity.Port = 444
	entity.Init(0)
	testEndpointMatch(t, ep, entity, Permitted)

	entity.Port = 445
	entity.Init(0)
	testEndpointMatch(t, ep, entity, NoMatch)

	testEndpointMatch(t, ep, (&intel.Entity{
		Domain: "example.com.",
	}).Init(0), NoMatch)

	// IP

	ep, err = parseEndpoint("+ 10.2.3.4")
	if err != nil {
		t.Fatal(err)
	}

	testEndpointMatch(t, ep, (&intel.Entity{
		Domain:   "",
		IP:       net.ParseIP("10.2.3.4"),
		Protocol: 6,
		Port:     443,
	}).Init(0), Permitted)
	testEndpointMatch(t, ep, (&intel.Entity{
		Domain:   "example.com.",
		IP:       net.ParseIP("10.2.3.4"),
		Protocol: 17,
		Port:     443,
	}).Init(0), Permitted)

	testEndpointMatch(t, ep, (&intel.Entity{
		Domain:   "",
		IP:       net.ParseIP("10.2.3.3"),
		Protocol: 6,
		Port:     443,
	}).Init(0), NoMatch)
	testEndpointMatch(t, ep, (&intel.Entity{
		Domain:   "example.com.",
		IP:       net.ParseIP("10.2.3.5"),
		Protocol: 17,
		Port:     443,
	}).Init(0), NoMatch)

	testEndpointMatch(t, ep, (&intel.Entity{
		Domain: "example.com.",
	}).Init(0), NoMatch)

	// IP Range

	ep, err = parseEndpoint("+ 10.2.3.0/24")
	if err != nil {
		t.Fatal(err)
	}
	testEndpointMatch(t, ep, (&intel.Entity{
		IP: net.ParseIP("10.2.2.4"),
	}).Init(0), NoMatch)
	testEndpointMatch(t, ep, (&intel.Entity{
		IP: net.ParseIP("10.2.3.4"),
	}).Init(0), Permitted)
	testEndpointMatch(t, ep, (&intel.Entity{
		IP: net.ParseIP("10.2.4.4"),
	}).Init(0), NoMatch)

	// Skip test that need the geoip database in CI.
	if !testing.Short() {

		// ASN

		ep, err = parseEndpoint("+ AS15169")
		if err != nil {
			t.Fatal(err)
		}

		entity = (&intel.Entity{IP: net.IPv4(8, 8, 8, 8)}).Init(0)
		testEndpointMatch(t, ep, entity, Permitted)

		entity = (&intel.Entity{IP: net.IPv4(1, 1, 1, 1)}).Init(0)
		testEndpointMatch(t, ep, entity, NoMatch)

		// Country

		ep, err = parseEndpoint("+ AT")
		if err != nil {
			t.Fatal(err)
		}

		entity = (&intel.Entity{IP: net.IPv4(194, 232, 104, 1)}).Init(0) // orf.at
		testEndpointMatch(t, ep, entity, Permitted)

		entity = (&intel.Entity{IP: net.IPv4(151, 101, 1, 164)}).Init(0) // nytimes.com
		testEndpointMatch(t, ep, entity, NoMatch)

	}

	// Scope

	ep, err = parseEndpoint("+ Localhost,LAN")
	if err != nil {
		t.Fatal(err)
	}

	entity = (&intel.Entity{IP: net.IPv4(192, 168, 0, 1)}).Init(0)
	testEndpointMatch(t, ep, entity, Permitted)

	entity = (&intel.Entity{IP: net.IPv4(151, 101, 1, 164)}).Init(0) // nytimes.com
	testEndpointMatch(t, ep, entity, NoMatch)

	// Port with protocol wildcard

	ep, err = parseEndpoint("+ * */443")
	if err != nil {
		t.Fatal(err)
	}
	entity = &intel.Entity{
		Domain:   "",
		IP:       net.ParseIP("10.2.3.4"),
		Protocol: 6,
		Port:     443,
	}
	entity.Init(0)
	testEndpointMatch(t, ep, entity, Permitted)

	// Lists

	// Skip test that need the filter lists in CI.
	if !testing.Short() {
		_, err = parseEndpoint("+ L:A,B,C")
		if err != nil {
			t.Fatal(err)
		}
	}

	// TODO: write test for lists matcher
}

func getLineNumberOfCaller(levels int) int {
	_, _, line, _ := runtime.Caller(levels + 1) //nolint:dogsled
	return line
}

func TestEndpointMatchingWithoutGeoIPData(t *testing.T) {
	t.Parallel()

	// An entity with a preset LocationError makes GetASN and GetCountryInfo
	// report missing data, regardless of the geoip database state.
	// The entities are reused, as each new entity waits for its geoip lookup.
	noData := func(protocol uint8, port uint16) *intel.Entity {
		return (&intel.Entity{
			IP:            net.ParseIP("104.82.234.109"),
			Protocol:      protocol,
			Port:          port,
			LocationError: "geoip data not available (test)",
		}).Init(0)
	}
	tcp443 := noData(6, 443)
	udp27015 := noData(17, 27015)

	for _, tc := range []struct {
		rule     string
		entity   *intel.Entity
		expected EPResult
	}{
		// Allow rules that cannot be evaluated are skipped.
		{"+ AS32590", tcp443, NoMatch},
		{"+ AS32590 UDP/27015-27250", udp27015, NoMatch},
		{"+ DE", tcp443, NoMatch},
		{"+ C:EU", tcp443, NoMatch},

		// Deny rules that cannot be evaluated fail safe.
		{"- AS32590", tcp443, MatchError},
		{"- DE", tcp443, MatchError},
		{"- C:EU", tcp443, MatchError},

		// Rules whose protocol/port do not fit never apply, even without data.
		{"+ AS32590 UDP/27015-27250", tcp443, NoMatch}, // Case from issue #2138.
		{"- AS32590 UDP/27015-27250", tcp443, NoMatch},
		{"- DE TCP/80", tcp443, NoMatch},
	} {
		ep, err := parseEndpoint(tc.rule)
		if err != nil {
			t.Fatal(err)
		}
		testEndpointMatch(t, ep, tc.entity, tc.expected)
	}

	// A skipped allow rule lets the following rules decide.
	list, err := ParseEndpoints([]string{"+ AS32590", "- *"})
	if err != nil {
		t.Fatal(err)
	}
	result, _ := list.Match(context.TODO(), tcp443)
	assert.Equal(t, Denied, result)
}
