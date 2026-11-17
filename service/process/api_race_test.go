package process

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"

	"github.com/safing/portmaster/base/api"
	"github.com/safing/portmaster/service/profile"
	"github.com/safing/structures/dsd"
)

const (
	apiTestProfileSource = profile.SourceLocal
	apiTestProfileID     = "api-race-test-profile"
)

// newAPITestProcess returns a fully populated process that is stored in the
// process storage (and thus reachable from the API handlers), together with
// a function that removes it again.
func newAPITestProcess(t *testing.T) (*Process, func()) {
	t.Helper()

	lp := profile.NewLayeredProfile(&profile.Profile{
		Source: apiTestProfileSource,
		ID:     apiTestProfileID,
	})

	p := &Process{
		Name:            "test",
		UserID:          1000,
		UserName:        "user",
		UserHome:        "/home/user",
		Pid:             4242,
		CreatedAt:       1700000000000,
		ParentPid:       1,
		ParentCreatedAt: 1600000000000,
		LeaderPid:       4242,
		Path:            "/usr/bin/test",
		ExecName:        "test",
		Cwd:             "/tmp",
		CmdLine:         "/usr/bin/test --flag",
		FirstArg:        "/usr/bin/test",
		Env:             map[string]string{"HOME": "/home/user", "LANG": "C"},
		processKey:      "4242-1700000000000",
		Tags:            []profile.Tag{{Key: "k", Value: "v"}},
		MatchingPath:    "/usr/bin/test-alt",
		FirstSeen:       1700000001,
		LastSeen:        1700000002,
		Error:           "some error",
		ExecHashes:      map[string]string{"sha256": "abcd"},
	}
	p.setProfile(profile.MakeScopedID(apiTestProfileSource, apiTestProfileID), lp)

	processesLock.Lock()
	processes[p.processKey] = p
	processesLock.Unlock()

	return p, func() {
		processesLock.Lock()
		delete(processes, p.processKey)
		processesLock.Unlock()
	}
}

func newProcessesByProfileRequest() *api.Request {
	return &api.Request{
		Request: httptest.NewRequest(http.MethodGet, "/api/v1/process/list/by-profile/local/"+apiTestProfileID, nil),
		URLVars: map[string]string{
			"source": string(apiTestProfileSource),
			"id":     apiTestProfileID,
		},
	}
}

// TestHandleGetProcessesByProfileRace checks that the processes returned by
// the API handler can be encoded (as the API endpoint does after the handler
// returns, without any lock) while the process is mutated under its lock, as
// CleanProcessStorage (SetLastSeen) and RefetchProfile (setProfile) do.
// Run with -race.
func TestHandleGetProcessesByProfileRace(t *testing.T) { //nolint:paralleltest // Modifies the global process storage.
	p, cleanup := newAPITestProcess(t)
	defer cleanup()
	lp := p.Profile()

	const rounds = 200
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Go(func() {
		for i := int64(0); ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			// Same write patterns as CleanProcessStorage and RefetchProfile.
			p.SetLastSeen(i)
			p.Lock()
			p.setProfile(profile.MakeScopedID(apiTestProfileSource, apiTestProfileID), lp)
			p.Unlock()
		}
	})

	for range rounds {
		// Same as the endpoint: call the StructFunc, then encode the result.
		v, err := handleGetProcessesByProfile(newProcessesByProfileRequest())
		if err != nil {
			t.Fatal(err)
		}
		_, _, _, err = dsd.MimeDump(v, "application/json")
		if err != nil {
			t.Fatal(err)
		}
	}
	close(stop)
	wg.Wait()
}

// TestProcessAPIOutputUnchanged checks that the API handler returns exactly
// the same encoded data as encoding the live process directly did.
func TestProcessAPIOutputUnchanged(t *testing.T) { //nolint:paralleltest // Modifies the global process storage.
	p, cleanup := newAPITestProcess(t)
	defer cleanup()

	// Make sure the test process has all serialized fields set, so that a field
	// missing from the snapshot changes the output.
	pv := reflect.ValueOf(p).Elem()
	for i := range pv.NumField() {
		f := pv.Type().Field(i)
		if f.IsExported() && !f.Anonymous && pv.Field(i).IsZero() {
			t.Fatalf("test process must set field %s", f.Name)
		}
	}

	formats := []string{"application/json", "application/yaml", "application/cbor", "application/msgpack"}
	for _, accept := range formats {
		// Encoded output of the live process, as previously returned by the handler.
		p.Lock()
		want, _, format, err := dsd.MimeDump([]*Process{p}, accept)
		p.Unlock()
		if err != nil {
			t.Fatalf("%s: %s", accept, err)
		}

		v, err := handleGetProcessesByProfile(newProcessesByProfileRequest())
		if err != nil {
			t.Fatal(err)
		}
		got, _, _, err := dsd.MimeDump(v, accept)
		if err != nil {
			t.Fatalf("%s: %s", accept, err)
		}
		assertSameEncoding(t, accept, format, want, got)

		// Single process, as returned by the group leader handler.
		p.Lock()
		want, _, _, err = dsd.MimeDump(p, accept)
		p.Unlock()
		if err != nil {
			t.Fatalf("%s: %s", accept, err)
		}
		got, _, _, err = dsd.MimeDump(p.snapshotForAPI(), accept)
		if err != nil {
			t.Fatalf("%s: %s", accept, err)
		}
		assertSameEncoding(t, accept, format, want, got)
	}
}

// assertSameEncoding checks that want and got are the same encoded data.
// CBOR and msgpack encode maps in random order, so their output is compared
// after decoding it.
func assertSameEncoding(t *testing.T, accept string, format uint8, want, got []byte) {
	t.Helper()

	if bytes.Equal(want, got) {
		return
	}
	if format == dsd.JSON || format == dsd.YAML {
		t.Errorf("%s: output changed:\nwant: %s\ngot:  %s", accept, want, got)
		return
	}

	var wantV, gotV any
	err := dsd.LoadAsFormat(want, format, &wantV)
	if err != nil {
		t.Fatalf("%s: %s", accept, err)
	}
	err = dsd.LoadAsFormat(got, format, &gotV)
	if err != nil {
		t.Fatalf("%s: %s", accept, err)
	}
	if !reflect.DeepEqual(wantV, gotV) {
		t.Errorf("%s: output changed:\nwant: %v\ngot:  %v", accept, wantV, gotV)
	}
}
