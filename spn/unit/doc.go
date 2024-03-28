// Package unit provides a "work unit" scheduling system for handling data sets that traverse multiple workers / goroutines.
// The aim is to bind priority to a data set instead of a goroutine and split resources fairly among requests.
//
// Every "work" Unit is assigned an ever increasing ID and can be marked as "paused" or "high priority".
// The Scheduler always gives a clearance up to a certain ID. All units below this ID may be processed.
// High priority Units may always be processed.
//
// The Scheduler works with short slots and measures how many Units were finished in a slot.
// The "slot pace" holds an indication of the current Unit finishing speed per slot. It is only changed slowly (but boosts if too far away) in order to keep stabilize the system.
// The Scheduler then calculates the next unit ID limit to give clearance to for the next slot:
//
//	"finished units" + "slot pace" + "paused units" - "fraction of high priority units"
package unit
