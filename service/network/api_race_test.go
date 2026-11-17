package network

import (
	"net"
	"strconv"
	"sync"
	"testing"

	"github.com/tevino/abool"

	"github.com/safing/portmaster/base/utils/debug"
	"github.com/safing/portmaster/service/intel"
	"github.com/safing/portmaster/service/mgr"
)

const (
	debugTestProfileSource = "local"
	debugTestProfileID     = "profile-a"
)

var setupDebugTestDBOnce sync.Once

// setupDebugTestDB injects the network database, so that
// AddNetworkDebugData can query the connection store.
func setupDebugTestDB(t *testing.T) {
	t.Helper()

	setupDebugTestDBOnce.Do(func() {
		if module == nil {
			module = &Network{mgr: mgr.New("NetworkTest")}
		}
		if dbController == nil {
			err := registerAsDatabase()
			if err != nil {
				t.Fatalf("failed to register network database: %s", err)
			}
		}
	})
	if dbController == nil {
		t.Fatal("network database is not available")
	}
}

// newDebugTestConnection returns a complete connection that is added to the
// connection store. The returned function removes it again.
func newDebugTestConnection(id string) (*Connection, func()) {
	conn := &Connection{
		ID:           id,
		Type:         IPConnection,
		IPProtocol:   6,
		LocalIP:      net.ParseIP("127.0.0.1"),
		LocalPort:    1234,
		Entity:       &intel.Entity{IP: net.ParseIP("127.0.0.2"), Port: 443},
		Verdict:      VerdictAccept,
		Started:      1,
		dataComplete: abool.NewBool(true),
		ProcessContext: ProcessContext{
			Source:  debugTestProfileSource,
			Profile: debugTestProfileID,
			PID:     1000,
		},
	}
	conn.SetKey(makeKey(1000, dbScopeIP, conn.ID))
	conns.add(conn)
	return conn, func() { conns.delete(conn) }
}

// TestAddNetworkDebugDataRace checks that the network debug data can be
// collected while the packet handlers change the verdict of the connections.
// Run with -race.
func TestAddNetworkDebugDataRace(t *testing.T) { //nolint:paralleltest // Modifies the global connection store.
	setupDebugTestDB(t)

	testConns := make([]*Connection, 0, 10)
	for i := range 10 {
		conn, remove := newDebugTestConnection("debug-data-race-" + strconv.Itoa(i))
		defer remove()
		testConns = append(testConns, conn)
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Go(func() {
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			for _, conn := range testConns {
				// Like the filter handler and a re-evaluation of the verdict.
				conn.Lock()
				if i%2 == 0 {
					conn.SetVerdict(VerdictBlock, "blocked", "", nil)
				} else {
					conn.SetVerdict(VerdictAccept, "allowed", "", nil)
				}
				conn.BytesReceived += 100
				conn.Unlock()
			}
		}
	})

	for range 50 {
		di := new(debug.Info)
		AddNetworkDebugData(di, "", "")
	}
	close(stop)
	wg.Wait()
}

// TestBuildNetworkDebugInfoDataRace checks that the network debug data can
// be formatted while connections are re-attributed to another profile.
// Run with -race.
func TestBuildNetworkDebugInfoDataRace(t *testing.T) {
	t.Parallel()

	testConns := make([]*Connection, 0, 10)
	for i := range 10 {
		testConns = append(testConns, &Connection{
			ID:           "debug-info-race-" + strconv.Itoa(i),
			Type:         IPConnection,
			Entity:       &intel.Entity{},
			Started:      int64(i),
			dataComplete: abool.NewBool(true),
			ProcessContext: ProcessContext{
				Source:  debugTestProfileSource,
				Profile: debugTestProfileID,
				PID:     1000 + i%3,
			},
		})
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Go(func() {
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			for _, conn := range testConns {
				// Like reAttributeConnection.
				conn.Lock()
				if i%2 == 0 {
					conn.ProcessContext = ProcessContext{Source: debugTestProfileSource, Profile: "profile-with-a-longer-id-b", PID: conn.ProcessContext.PID}
				} else {
					conn.ProcessContext = ProcessContext{Source: debugTestProfileSource, Profile: debugTestProfileID, PID: conn.ProcessContext.PID}
				}
				conn.Unlock()
			}
		}
	})

	for range 50 {
		debugConns := make([]*Connection, len(testConns))
		copy(debugConns, testConns)
		if buildNetworkDebugInfoData(debugConns) == "" {
			t.Error("debug info must not be empty")
			break
		}
	}
	close(stop)
	wg.Wait()
}
