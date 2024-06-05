package rng

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"sync"

	"github.com/aead/serpent"
	"github.com/seehuhn/fortuna"

	"github.com/safing/portmaster/base/modules"
)

var (
	rng      *fortuna.Generator
	rngLock  sync.Mutex
	rngReady = false

	rngCipher = "aes"
	// Possible values: "aes", "serpent".

	module *modules.Module
)

func init() {
	module = modules.Register("rng", nil, start, nil)
}

func newCipher(key []byte) (cipher.Block, error) {
	switch rngCipher {
	case "aes":
		return aes.NewCipher(key)
	case "serpent":
		return serpent.NewCipher(key)
	default:
		return nil, fmt.Errorf("unknown or unsupported cipher: %s", rngCipher)
	}
}

func start() error {
	rngLock.Lock()
	defer rngLock.Unlock()

	rng = fortuna.NewGenerator(newCipher)
	if rng == nil {
		return errors.New("failed to initialize rng")
	}

	// add another (async) OS rng seed
	module.StartWorker("initial rng feed", func(_ context.Context) error {
		// get entropy from OS
		osEntropy := make([]byte, minFeedEntropy/8)
		_, err := rand.Read(osEntropy)
		if err != nil {
			return fmt.Errorf("could not read entropy from os: %w", err)
		}
		// feed
		rngLock.Lock()
		rng.Reseed(osEntropy)
		rngLock.Unlock()
		return nil
	})

	// mark as ready
	rngReady = true

	// random source: OS
	module.StartServiceWorker("os rng feeder", 0, osFeeder)

	// random source: goroutine ticks
	module.StartServiceWorker("tick rng feeder", 0, tickFeeder)

	// full feeder
	module.StartServiceWorker("full feeder", 0, fullFeeder)

	return nil
}
