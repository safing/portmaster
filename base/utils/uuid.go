package utils

import (
	"encoding/binary"
	"time"

	"github.com/gofrs/uuid"
)

var (
	constantUUID = uuid.Must(uuid.FromString("e8dba9f7-21e2-4c82-96cb-6586922c6422"))
	instanceUUID = RandomUUID("instance")
)

// RandomUUID returns a new random UUID with optionally provided ns.
func RandomUUID(ns string) uuid.UUID {
	randUUID, err := uuid.NewV4()
	switch {
	case err != nil:
		// fallback
		// should practically never happen
		return uuid.NewV5(uuidFromTime(), ns)
	case ns != "":
		// mix ns into the UUID
		return uuid.NewV5(randUUID, ns)
	default:
		return randUUID
	}
}

// DerivedUUID returns a new UUID that is derived from the input only, and therefore is always reproducible.
func DerivedUUID(input string) uuid.UUID {
	return uuid.NewV5(constantUUID, input)
}

// DerivedInstanceUUID returns a new UUID that is derived from the input, but is unique per instance (execution) and therefore is only reproducible with the same process.
func DerivedInstanceUUID(input string) uuid.UUID {
	return uuid.NewV5(instanceUUID, input)
}

func uuidFromTime() uuid.UUID {
	var timeUUID uuid.UUID
	binary.LittleEndian.PutUint64(timeUUID[:], uint64(time.Now().UnixNano()))
	return timeUUID
}
