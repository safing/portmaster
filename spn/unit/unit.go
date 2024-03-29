package unit

import (
	"time"

	"github.com/tevino/abool"
)

// Unit describes a "work unit" and is meant to be embedded into another struct
// used for passing data moving through multiple processing steps.
type Unit struct {
	id           int64
	scheduler    *Scheduler
	created      time.Time
	finished     abool.AtomicBool
	highPriority abool.AtomicBool
}

// NewUnit returns a new unit within the scheduler.
func (s *Scheduler) NewUnit() *Unit {
	return &Unit{
		id:        s.currentUnitID.Add(1),
		scheduler: s,
		created:   time.Now(),
	}
}

// ReUse re-initialized the unit to be able to reuse already allocated structs.
func (u *Unit) ReUse() {
	// Finish previous unit.
	u.Finish()

	// Get new ID and unset finish flag.
	u.id = u.scheduler.currentUnitID.Add(1)
	u.finished.UnSet()
}

// WaitForSlot blocks until the unit may be processed.
func (u *Unit) WaitForSlot() {
	// High priority units may always process.
	if u.highPriority.IsSet() {
		return
	}

	for {
		// Check if we are allowed to process in the current slot.
		if u.id <= u.scheduler.clearanceUpTo.Load() {
			return
		}

		// Debug logging:
		// fmt.Printf("unit %d waiting for clearance at %d\n", u.id, u.scheduler.clearanceUpTo.Load())

		// Wait for next slot.
		<-u.scheduler.nextSlotSignal()
	}
}

// Finish signals the unit scheduler that this unit has finished processing.
// Will no-op if called on a nil Unit.
func (u *Unit) Finish() {
	if u == nil {
		return
	}

	// Always increase finished, even if the unit is from a previous epoch.
	if u.finished.SetToIf(false, true) {
		u.scheduler.finished.Add(1)

		// Record the time this unit took from creation to finish.
		timeTaken := time.Since(u.created).Nanoseconds()
		u.scheduler.stats.progress.avgUnitLifeCnt.Add(1)
		if u.scheduler.stats.progress.avgUnitLifeSum.Add(timeTaken) < 0 {
			// Reset if we wrap.
			u.scheduler.stats.progress.avgUnitLifeCnt.Store(1)
			u.scheduler.stats.progress.avgUnitLifeSum.Store(timeTaken)
		}
	}
}

// MakeHighPriority marks the unit as high priority.
func (u *Unit) MakeHighPriority() {
	switch {
	case u.finished.IsSet():
		// Unit is already finished.
	case !u.highPriority.SetToIf(false, true):
		// Unit is already set to high priority.
		// Else: High Priority set.
	case u.id > u.scheduler.clearanceUpTo.Load():
		// Unit is outside current clearance, reduce clearance by one.
		u.scheduler.clearanceUpTo.Add(-1)
	}
}

// IsHighPriority returns whether the unit has high priority.
func (u *Unit) IsHighPriority() bool {
	return u.highPriority.IsSet()
}

// RemovePriority removes the high priority mark.
func (u *Unit) RemovePriority() {
	u.highPriority.UnSet()
}
