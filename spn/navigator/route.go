package navigator

import (
	"fmt"
	mrand "math/rand"
	"sort"
	"strings"
	"time"
)

// Routes holds a collection of Routes.
type Routes struct {
	All                 []*Route
	randomizeTopPercent float32
	maxCost             float32 // automatic
	maxRoutes           int     // manual setting
}

// Len is the number of elements in the collection.
func (r *Routes) Len() int {
	return len(r.All)
}

// Less reports whether the element with index i should sort before the element
// with index j.
func (r *Routes) Less(i, j int) bool {
	return r.All[i].TotalCost < r.All[j].TotalCost
}

// Swap swaps the elements with indexes i and j.
func (r *Routes) Swap(i, j int) {
	r.All[i], r.All[j] = r.All[j], r.All[i]
}

// isGoodEnough reports whether the route would survive a clean process.
func (r *Routes) isGoodEnough(route *Route) bool {
	if r.maxCost > 0 && route.TotalCost > r.maxCost {
		return false
	}
	return true
}

// add adds a Route if it is good enough.
func (r *Routes) add(route *Route) {
	if !r.isGoodEnough(route) {
		return
	}
	r.All = append(r.All, route.CopyUpTo(0))
	r.clean()
}

// clean sort and shortens the list to the configured maximum.
func (r *Routes) clean() {
	// Sort Routes so that the best ones are on top.
	sort.Sort(r)
	// Remove all remaining from the list.
	if len(r.All) > r.maxRoutes {
		r.All = r.All[:r.maxRoutes]
	}
	// Set new maximum total cost.
	if len(r.All) >= r.maxRoutes {
		r.maxCost = r.All[len(r.All)-1].TotalCost
	}
}

// randomizeTop randomized to the top nearest pins for balancing the network.
func (r *Routes) randomizeTop() {
	switch {
	case r.randomizeTopPercent == 0:
		// Check if randomization is enabled.
		return
	case len(r.All) < 2:
		// Check if we have enough pins to work with.
		return
	}

	// Find randomization set.
	randomizeUpTo := len(r.All)
	threshold := r.All[0].TotalCost * (1 + r.randomizeTopPercent)
	for i, r := range r.All {
		// Find first value above the threshold to stop.
		if r.TotalCost > threshold {
			randomizeUpTo = i
			break
		}
	}

	// Shuffle top set.
	if randomizeUpTo >= 2 {
		mr := mrand.New(mrand.NewSource(time.Now().UnixNano())) //nolint:gosec
		mr.Shuffle(randomizeUpTo, r.Swap)
	}
}

// Route is a path through the map.
type Route struct {
	// Path is a list of Transit Hubs and the Destination Hub, including the Cost
	// for each Hop.
	Path []*Hop

	// DstCost is the calculated cost between the Destination Hub and the destination IP.
	DstCost float32

	// TotalCost is the sum of all costs of this Route.
	TotalCost float32

	// Algorithm is the ID of the algorithm used to calculate the route.
	Algorithm string
}

// Hop is one hop of a route's path.
type Hop struct {
	pin *Pin

	// HubID is the Hub ID.
	HubID string

	// Cost is the cost for both Lane to this Hub and the Hub itself.
	Cost float32
}

// addHop adds a hop to the route.
func (r *Route) addHop(pin *Pin, cost float32) {
	r.Path = append(r.Path, &Hop{
		pin:  pin,
		Cost: cost,
	})
	r.recalculateTotalCost()
}

// completeRoute completes the route by adding the destination cost of the
// connection between the last hop and the destination IP.
func (r *Route) completeRoute(dstCost float32) {
	r.DstCost = dstCost
	r.recalculateTotalCost()
}

// removeHop removes the last hop from the Route.
func (r *Route) removeHop() {
	// Reset DstCost, as the route might have been completed.
	r.DstCost = 0

	if len(r.Path) >= 1 {
		r.Path = r.Path[:len(r.Path)-1]
	}
	r.recalculateTotalCost()
}

// recalculateTotalCost recalculates to total cost of this route.
func (r *Route) recalculateTotalCost() {
	r.TotalCost = r.DstCost
	for _, hop := range r.Path {
		if hop.pin.HasActiveTerminal() {
			// If we have an active connection, only take 80% of the cost.
			r.TotalCost += hop.Cost * 0.8
		} else {
			r.TotalCost += hop.Cost
		}
	}
}

// CopyUpTo makes a somewhat deep copy of the Route up to the specified amount
// and returns it. Hops themselves are not copied, because their data does not
// change. Therefore, returned Hops may not be edited.
// Specify an amount of 0 to copy all.
func (r *Route) CopyUpTo(n int) *Route {
	// Check amount.
	if n == 0 || n > len(r.Path) {
		n = len(r.Path)
	}

	newRoute := &Route{
		Path:      make([]*Hop, n),
		DstCost:   r.DstCost,
		TotalCost: r.TotalCost,
	}
	copy(newRoute.Path, r.Path)
	return newRoute
}

// makeExportReady fills in all the missing data fields which are meant for
// exporting only.
func (r *Routes) makeExportReady(algorithm string) {
	for _, route := range r.All {
		route.makeExportReady(algorithm)
	}
}

// makeExportReady fills in all the missing data fields which are meant for
// exporting only.
func (r *Route) makeExportReady(algorithm string) {
	r.Algorithm = algorithm
	for _, hop := range r.Path {
		hop.makeExportReady()
	}
}

// makeExportReady fills in all the missing data fields which are meant for
// exporting only.
func (hop *Hop) makeExportReady() {
	hop.HubID = hop.pin.Hub.ID
}

// Pin returns the Pin of the Hop.
func (hop *Hop) Pin() *Pin {
	return hop.pin
}

func (r *Route) String() string {
	s := make([]string, 0, len(r.Path)+2)
	s = append(s, fmt.Sprintf("route with %.2fc:", r.TotalCost))
	for i, hop := range r.Path {
		if i == 0 {
			s = append(s, hop.pin.String())
		} else {
			s = append(s, fmt.Sprintf("--> %.2fc %s", hop.Cost, hop.pin))
		}
	}
	s = append(s, fmt.Sprintf("--> %.2fc", r.DstCost))
	return strings.Join(s, " ")
}
