package rng

import (
	"encoding/binary"
	"errors"
	"io"
	"math"
	"time"
)

const (
	reseedAfterSeconds = 600     // ten minutes
	reseedAfterBytes   = 1048576 // one megabyte
)

var (
	// Reader provides a global instance to read from the RNG.
	Reader io.Reader

	rngBytesRead uint64
	rngLastFeed  = time.Now()
)

// reader provides an io.Reader interface.
type reader struct{}

func init() {
	Reader = reader{}
}

func checkEntropy() (err error) {
	if !rngReady {
		return errors.New("RNG is not ready yet")
	}
	if rngBytesRead > reseedAfterBytes ||
		int(time.Since(rngLastFeed).Seconds()) > reseedAfterSeconds {
		select {
		case r := <-rngFeeder:
			rng.Reseed(r)
			rngBytesRead = 0
			rngLastFeed = time.Now()
		case <-time.After(1 * time.Second):
			return errors.New("failed to get new entropy")
		}
	}
	return nil
}

// Read reads random bytes into the supplied byte slice.
func Read(b []byte) (n int, err error) {
	rngLock.Lock()
	defer rngLock.Unlock()

	if err := checkEntropy(); err != nil {
		return 0, err
	}

	return copy(b, rng.PseudoRandomData(uint(len(b)))), nil
}

// Read implements the io.Reader interface.
func (r reader) Read(b []byte) (n int, err error) {
	return Read(b)
}

// Bytes allocates a new byte slice of given length and fills it with random data.
func Bytes(n int) ([]byte, error) {
	rngLock.Lock()
	defer rngLock.Unlock()

	if err := checkEntropy(); err != nil {
		return nil, err
	}

	return rng.PseudoRandomData(uint(n)), nil
}

// Number returns a random number from 0 to (incl.) max.
func Number(max uint64) (uint64, error) {
	secureLimit := math.MaxUint64 - (math.MaxUint64 % max)
	max++

	for {
		randomBytes, err := Bytes(8)
		if err != nil {
			return 0, err
		}

		candidate := binary.LittleEndian.Uint64(randomBytes)
		if candidate < secureLimit {
			return candidate % max, nil
		}
	}
}
