package hub

import (
	"sync"
	"time"

	"github.com/tevino/abool"
)

// MaxCalculatedCost specifies the max calculated cost to be used for an unknown high cost.
const MaxCalculatedCost = 1000000

// Measurements holds various measurements relating to a Hub.
// Fields may not be accessed directly.
type Measurements struct {
	sync.Mutex

	// Latency designates the latency between these Hubs.
	// It is specified in nanoseconds.
	Latency time.Duration
	// LatencyMeasuredAt holds when the latency was measured.
	LatencyMeasuredAt time.Time

	// Capacity designates the available bandwidth between these Hubs.
	// It is specified in bit/s.
	Capacity int
	// CapacityMeasuredAt holds when the capacity measurement expires.
	CapacityMeasuredAt time.Time

	// CalculatedCost stores the calculated cost for direct access.
	// It is not set automatically, but needs to be set when needed.
	CalculatedCost float32

	// GeoProximity stores an approximation of the geolocation proximity.
	// The value is between 0 (other side of the world) and 100 (same location).
	GeoProximity float32

	// persisted holds whether the Measurements have been persisted to the
	// database.
	persisted *abool.AtomicBool
}

// NewMeasurements returns a new measurements struct.
func NewMeasurements() *Measurements {
	m := &Measurements{
		CalculatedCost: MaxCalculatedCost, // Push to back when sorting without data.
	}
	m.check()
	return m
}

// Copy returns a copy of the measurements.
func (m *Measurements) Copy() *Measurements {
	copied := &Measurements{
		Latency:            m.Latency,
		LatencyMeasuredAt:  m.LatencyMeasuredAt,
		Capacity:           m.Capacity,
		CapacityMeasuredAt: m.CapacityMeasuredAt,
		CalculatedCost:     m.CalculatedCost,
	}
	copied.check()
	return copied
}

// Check checks if the Measurements are properly initialized and ready to use.
func (m *Measurements) check() {
	if m == nil {
		return
	}

	m.Lock()
	defer m.Unlock()

	if m.persisted == nil {
		m.persisted = abool.NewBool(true)
	}
}

// IsPersisted return whether changes to the measurements have been persisted.
func (m *Measurements) IsPersisted() bool {
	return m.persisted.IsSet()
}

// Valid returns whether there is a valid value .
func (m *Measurements) Valid() bool {
	m.Lock()
	defer m.Unlock()

	switch {
	case m.Latency == 0:
		// Latency is not set.
	case m.Capacity == 0:
		// Capacity is not set.
	case m.CalculatedCost == 0:
		// CalculatedCost is not set.
	case m.CalculatedCost == MaxCalculatedCost:
		// CalculatedCost is set to static max value.
	default:
		return true
	}

	return false
}

// Expired returns whether any of the measurements has expired - calculated
// with the given TTL.
func (m *Measurements) Expired(ttl time.Duration) bool {
	expiry := time.Now().Add(-ttl)

	m.Lock()
	defer m.Unlock()

	switch {
	case expiry.After(m.LatencyMeasuredAt):
		return true
	case expiry.After(m.CapacityMeasuredAt):
		return true
	default:
		return false
	}
}

// SetLatency sets the latency to the given value.
func (m *Measurements) SetLatency(latency time.Duration) {
	m.Lock()
	defer m.Unlock()

	m.Latency = latency
	m.LatencyMeasuredAt = time.Now()
	m.persisted.UnSet()
}

// GetLatency returns the latency and when it expires.
func (m *Measurements) GetLatency() (latency time.Duration, measuredAt time.Time) {
	m.Lock()
	defer m.Unlock()

	return m.Latency, m.LatencyMeasuredAt
}

// SetCapacity sets the capacity to the given value.
// The capacity is measued in bit/s.
func (m *Measurements) SetCapacity(capacity int) {
	m.Lock()
	defer m.Unlock()

	m.Capacity = capacity
	m.CapacityMeasuredAt = time.Now()
	m.persisted.UnSet()
}

// GetCapacity returns the capacity and when it expires.
// The capacity is measued in bit/s.
func (m *Measurements) GetCapacity() (capacity int, measuredAt time.Time) {
	m.Lock()
	defer m.Unlock()

	return m.Capacity, m.CapacityMeasuredAt
}

// SetCalculatedCost sets the calculated cost to the given value.
// The calculated cost is not set automatically, but needs to be set when needed.
func (m *Measurements) SetCalculatedCost(cost float32) {
	m.Lock()
	defer m.Unlock()

	m.CalculatedCost = cost
	m.persisted.UnSet()
}

// GetCalculatedCost returns the calculated cost.
// The calculated cost is not set automatically, but needs to be set when needed.
func (m *Measurements) GetCalculatedCost() (cost float32) {
	if m == nil {
		return MaxCalculatedCost
	}

	m.Lock()
	defer m.Unlock()

	return m.CalculatedCost
}

// SetGeoProximity sets the geolocation proximity to the given value.
func (m *Measurements) SetGeoProximity(geoProximity float32) {
	m.Lock()
	defer m.Unlock()

	m.GeoProximity = geoProximity
	m.persisted.UnSet()
}

// GetGeoProximity returns the geolocation proximity.
func (m *Measurements) GetGeoProximity() (geoProximity float32) {
	if m == nil {
		return 0
	}

	m.Lock()
	defer m.Unlock()

	return m.GeoProximity
}

var (
	measurementsRegistry     = make(map[string]*Measurements)
	measurementsRegistryLock sync.Mutex
)

func getSharedMeasurements(hubID string, existing *Measurements) *Measurements {
	measurementsRegistryLock.Lock()
	defer measurementsRegistryLock.Unlock()

	// 1. Check registry and return shared measurements.
	m, ok := measurementsRegistry[hubID]
	if ok {
		return m
	}

	// 2. Use existing and make it shared, if available.
	if existing != nil {
		existing.check()
		measurementsRegistry[hubID] = existing
		return existing
	}

	// 3. Create new measurements.
	m = NewMeasurements()
	measurementsRegistry[hubID] = m
	return m
}
