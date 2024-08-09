package rng

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/aead/serpent"
	"github.com/seehuhn/fortuna"

	"github.com/safing/portmaster/service/mgr"
)

// Rng is a random number generator.
type Rng struct {
	mgr *mgr.Manager

	instance instance
}

var (
	rng      *fortuna.Generator
	rngLock  sync.Mutex
	rngReady = false

	rngCipher = "aes"
	// Possible values: "aes", "serpent".
)

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

// Manager returns the module manager.
func (r *Rng) Manager() *mgr.Manager {
	return r.mgr
}

// Start starts the module.
func (r *Rng) Start() error {
	rngLock.Lock()
	defer rngLock.Unlock()

	rng = fortuna.NewGenerator(newCipher)
	if rng == nil {
		return errors.New("failed to initialize rng")
	}

	// add another (async) OS rng seed
	r.mgr.Go("initial rng feed", func(_ *mgr.WorkerCtx) error {
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
	r.mgr.Go("os rng feeder", osFeeder)

	// random source: goroutine ticks
	r.mgr.Go("tick rng feeder", tickFeeder)

	// full feeder
	r.mgr.Go("full feeder", fullFeeder)

	return nil
}

// Stop stops the module.
func (r *Rng) Stop() error {
	return nil
}

var (
	module     *Rng
	shimLoaded atomic.Bool
)

// New returns a new rng.
func New(instance instance) (*Rng, error) {
	if !shimLoaded.CompareAndSwap(false, true) {
		return nil, errors.New("only one instance allowed")
	}
	m := mgr.New("Rng")
	module = &Rng{
		mgr:      m,
		instance: instance,
	}

	return module, nil
}

type instance interface{}
