package crew

import (
	"testing"
	"time"

	"github.com/safing/portmaster/spn/cabin"
	"github.com/safing/portmaster/spn/conf"
	"github.com/safing/portmaster/spn/docks"
	"github.com/safing/portmaster/spn/hub"
	"github.com/safing/portmaster/spn/navigator"
	"github.com/safing/portmaster/spn/ships"
	"github.com/safing/portmaster/spn/terminal"
)

// routeTestSetup holds a route from a home hub via a transit hub to a
// destination hub on the main map.
type routeTestSetup struct {
	route *navigator.Route

	home, transit, destination *cabin.Identity

	// homeTerminal is the terminal to the home hub. It belongs to a crane
	// that is never started, so everything sent through it is swallowed and
	// no replies arrive: expansions via the home hub are started successfully,
	// but authentication never completes.
	homeTerminal *docks.CraneTerminal
	homeCrane    *docks.Crane
}

// setupRouteTest creates three new hubs that are connected in a line, adds
// them to the main map, sets the first one as home hub and returns the route
// to the last one.
func setupRouteTest(t *testing.T) *routeTestSetup {
	t.Helper()

	s := &routeTestSetup{}

	// Create hubs with exchange keys.
	var err error
	for _, id := range []**cabin.Identity{&s.home, &s.transit, &s.destination} {
		*id, err = cabin.CreateIdentity(module.mgr.Ctx(), conf.MainMapName)
		if err != nil {
			t.Fatalf("failed to create identity: %s", err)
		}
	}

	// Connect them: home <-> transit <-> destination.
	lane := func(to *cabin.Identity) *hub.Lane {
		return &hub.Lane{
			ID:       to.Hub.ID,
			Latency:  10 * time.Millisecond,
			Capacity: 100_000_000,
		}
	}
	for _, link := range []struct {
		id    *cabin.Identity
		lanes []*hub.Lane
	}{
		{s.home, []*hub.Lane{lane(s.transit)}},
		{s.transit, []*hub.Lane{lane(s.home), lane(s.destination)}},
		{s.destination, []*hub.Lane{lane(s.transit)}},
	} {
		_, err = link.id.MaintainStatus(link.lanes, nil, nil, true)
		if err != nil {
			t.Fatalf("failed to set lanes: %s", err)
		}
	}
	for _, id := range []*cabin.Identity{s.home, s.transit, s.destination} {
		navigator.Main.UpdateHub(id.Hub)
	}
	// Add the lanes of the first hub again, as its peer was not yet on the map.
	navigator.Main.UpdateHub(s.home.Hub)

	// Create the terminal to the home hub.
	s.homeCrane, err = docks.NewCrane(ships.NewTestShip(true, 100), s.home.Hub, nil)
	if err != nil {
		t.Fatalf("failed to create crane: %s", err)
	}
	var tErr *terminal.Error
	s.homeTerminal, _, tErr = docks.NewLocalCraneTerminal(s.homeCrane, nil, terminal.DefaultHomeHubTerminalOpts())
	if tErr != nil {
		t.Fatalf("failed to create crane terminal: %s", tErr)
	}
	t.Cleanup(func() {
		s.homeTerminal.Abandon(nil)
		s.homeCrane.Stop(nil)
	})

	// Set home hub and find the route.
	if !navigator.Main.SetHome(s.home.Hub.ID, s.homeTerminal) {
		t.Fatal("failed to set home hub")
	}
	routes, err := navigator.Main.FindRouteToHub(s.destination.Hub.ID, navigator.Main.DefaultOptions())
	if err != nil {
		for _, id := range []*cabin.Identity{s.home, s.transit, s.destination} {
			pin, _ := navigator.Main.GetPin(id.Hub.ID)
			t.Logf("%s: states=%s hop distance=%d lanes=%d", pin, pin.State, pin.HopDistance, len(pin.ConnectedTo))
		}
		t.Fatalf("failed to find route: %s", err)
	}
	if len(routes.All) == 0 {
		t.Fatal("no route found")
	}
	s.route = routes.All[0]
	if len(s.route.Path) != 3 {
		t.Fatalf("unexpected route: %s", s.route)
	}

	return s
}

// waitForNoActiveOps waits until the given terminal has no active operations.
func waitForNoActiveOps(t *testing.T, term *docks.CraneTerminal, within time.Duration) {
	t.Helper()

	started := time.Now()
	for term.GetActiveOpCount() > 0 {
		if time.Since(started) > within {
			t.Fatalf(
				"%d operations still active on %s after %s: expansions were not abandoned",
				term.GetActiveOpCount(), term.FmtID(), time.Since(started),
			)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestEstablishRouteAbandonsExpansionsOnAuthTimeout checks that establishRoute
// abandons the expansions it created when authenticating to a hop times out.
func TestEstablishRouteAbandonsExpansionsOnAuthTimeout(t *testing.T) { //nolint:paralleltest // Uses the home hub of the main map.
	s := setupRouteTest(t)

	// Both expansions can be started, as nothing waits for a reply. But the
	// authentication to the transit hub never completes.
	_, _, err := establishRoute(s.route)
	if err == nil {
		t.Fatal("establishing the route should fail")
	}
	t.Logf("establishing route failed as expected: %s", err)

	// The expansion to the transit hub runs as an operation on the home hub
	// terminal. Abandoning the expansion stops this operation. Without cleanup,
	// it would only be stopped by the idle timeout of the expansion terminal.
	waitForNoActiveOps(t, s.homeTerminal, 5*time.Second)

	// No expansion may have been taken into use.
	for _, hop := range s.route.Path[1:] {
		if hop.Pin().GetActiveTerminal() != nil {
			t.Errorf("%s has an active terminal", hop.Pin())
		}
	}
}

// TestEstablishRouteAbandonsExpansionsOnExpandFailure checks that
// establishRoute abandons the expansions it created when expanding to a later
// hop fails.
func TestEstablishRouteAbandonsExpansionsOnExpandFailure(t *testing.T) { //nolint:paralleltest // Uses the home hub of the main map.
	s := setupRouteTest(t)

	// Remove the exchange keys of the destination hub after the route was
	// found, so that no encrypted expansion to it can be created.
	func() {
		s.destination.Hub.Lock()
		defer s.destination.Hub.Unlock()

		status := s.destination.Hub.Status.Copy()
		status.Keys = nil
		s.destination.Hub.Status = status
	}()

	// The expansion to the transit hub is started successfully, the one to the
	// destination hub fails before any authentication result is awaited.
	started := time.Now()
	_, _, err := establishRoute(s.route)
	if err == nil {
		t.Fatal("establishing the route should fail")
	}
	t.Logf("establishing route failed as expected after %s: %s", time.Since(started), err)

	// The expansion to the transit hub must be abandoned.
	waitForNoActiveOps(t, s.homeTerminal, 5*time.Second)

	if s.transit.Hub.ID != s.route.Path[1].HubID {
		t.Fatalf("unexpected transit hub in route %s", s.route)
	}
	if pin := s.route.Path[1].Pin(); pin.GetActiveTerminal() != nil {
		t.Errorf("%s has an active terminal", pin)
	}
}
