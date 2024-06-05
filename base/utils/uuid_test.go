package utils

import (
	"testing"
	"time"

	"github.com/gofrs/uuid"
)

func TestUUID(t *testing.T) {
	t.Parallel()

	// check randomness
	a := RandomUUID("")
	a2 := RandomUUID("")
	if a.String() == a2.String() {
		t.Error("should not match")
	}

	// check with input
	b := RandomUUID("b")
	b2 := RandomUUID("b")
	if b.String() == b2.String() {
		t.Error("should not match")
	}

	// check with long input
	c := RandomUUID("TG8UkxS+4rVrDxHtDAaNab1CBpygzmX1g5mJA37jbQ5q2uE4rVrDxHtDAaNab1CBpygzmX1g5mJA37jbQ5q2uE")
	c2 := RandomUUID("TG8UkxS+4rVrDxHtDAaNab1CBpygzmX1g5mJA37jbQ5q2uE4rVrDxHtDAaNab1CBpygzmX1g5mJA37jbQ5q2uE")
	if c.String() == c2.String() {
		t.Error("should not match")
	}

	// check for nanosecond precision
	d := uuidFromTime()
	time.Sleep(2 * time.Nanosecond)
	d2 := uuidFromTime()
	if d.String() == d2.String() {
		t.Error("should not match")
	}

	// check mixing
	timeUUID := uuidFromTime()
	e := uuid.NewV5(timeUUID, "e")
	e2 := uuid.NewV5(timeUUID, "e2")
	if e.String() == e2.String() {
		t.Error("should not match")
	}

	// check deriving
	f := DerivedUUID("f")
	f2 := DerivedUUID("f")
	f3 := DerivedUUID("f3")
	if f.String() != f2.String() {
		t.Error("should match")
	}
	if f.String() == f3.String() {
		t.Error("should not match")
	}

	// check instance deriving
	g := DerivedInstanceUUID("g")
	g2 := DerivedInstanceUUID("g")
	g3 := DerivedInstanceUUID("g3")
	if g.String() != g2.String() {
		t.Error("should match")
	}
	if g.String() == g3.String() {
		t.Error("should not match")
	}
}
