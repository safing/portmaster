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
	"github.com/safing/portmaster/service/mgr"
	"github.com/seehuhn/fortuna"
)

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

func (r *Rng) Start(m *mgr.Manager) error {
	r.mgr = m
	rngLock.Lock()
	defer rngLock.Unlock()

	rng = fortuna.NewGenerator(newCipher)
	if rng == nil {
		return errors.New("failed to initialize rng")
	}

	// add another (async) OS rng seed
	m.Go("initial rng feed", func(_ *mgr.WorkerCtx) error {
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
	m.Go("os rng feeder", osFeeder)

	// random source: goroutine ticks
	m.Go("tick rng feeder", tickFeeder)

	// full feeder
	m.Go("full feeder", fullFeeder)

	return nil
}

func (r *Rng) Stop(m *mgr.Manager) error {
	return nil
}

var (
	module     *Rng
	shimLoaded atomic.Bool
)

func New(instance instance) (*Rng, error) {
	if !shimLoaded.CompareAndSwap(false, true) {
		return nil, errors.New("only one instance allowed")
	}

	module = &Rng{
		instance: instance,
	}

	return module, nil
}

type instance interface{}
