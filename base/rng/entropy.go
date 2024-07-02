package rng

import (
	"encoding/binary"

	"github.com/tevino/abool"

	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/structures/container"
)

const (
	minFeedEntropy = 256
)

var rngFeeder = make(chan []byte)

// The Feeder is used to feed entropy to the RNG.
type Feeder struct {
	input        chan *entropyData
	entropy      int64
	needsEntropy *abool.AtomicBool
	buffer       *container.Container
}

type entropyData struct {
	data    []byte
	entropy int
}

// NewFeeder returns a new entropy Feeder.
func NewFeeder() *Feeder {
	newFeeder := &Feeder{
		input:        make(chan *entropyData),
		needsEntropy: abool.NewBool(true),
		buffer:       container.New(),
	}
	module.mgr.Go("feeder", newFeeder.run)
	return newFeeder
}

// NeedsEntropy returns whether the feeder is currently gathering entropy.
func (f *Feeder) NeedsEntropy() bool {
	return f.needsEntropy.IsSet()
}

// SupplyEntropy supplies entropy to the Feeder, it will block until the Feeder has read from it.
func (f *Feeder) SupplyEntropy(data []byte, entropy int) {
	f.input <- &entropyData{
		data:    data,
		entropy: entropy,
	}
}

// SupplyEntropyIfNeeded supplies entropy to the Feeder, but will not block if no entropy is currently needed.
func (f *Feeder) SupplyEntropyIfNeeded(data []byte, entropy int) {
	if f.needsEntropy.IsSet() {
		return
	}

	select {
	case f.input <- &entropyData{
		data:    data,
		entropy: entropy,
	}:
	default:
	}
}

// SupplyEntropyAsInt supplies entropy to the Feeder, it will block until the Feeder has read from it.
func (f *Feeder) SupplyEntropyAsInt(n int64, entropy int) {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint64(b, uint64(n))
	f.SupplyEntropy(b, entropy)
}

// SupplyEntropyAsIntIfNeeded supplies entropy to the Feeder, but will not block if no entropy is currently needed.
func (f *Feeder) SupplyEntropyAsIntIfNeeded(n int64, entropy int) {
	if f.needsEntropy.IsSet() { // avoid allocating a slice if possible
		b := make([]byte, 8)
		binary.LittleEndian.PutUint64(b, uint64(n))
		f.SupplyEntropyIfNeeded(b, entropy)
	}
}

// CloseFeeder stops the feed processing - the responsible goroutine exits. The input channel is closed and the feeder may not be used anymore in any way.
func (f *Feeder) CloseFeeder() {
	close(f.input)
}

func (f *Feeder) run(ctx *mgr.WorkerCtx) error {
	defer f.needsEntropy.UnSet()

	for {
		// gather
		f.needsEntropy.Set()
	gather:
		for {
			select {
			case newEntropy := <-f.input:
				// check if feed has been closed
				if newEntropy == nil {
					return nil
				}
				// append to buffer
				f.buffer.Append(newEntropy.data)
				f.entropy += int64(newEntropy.entropy)
				if f.entropy >= minFeedEntropy {
					break gather
				}
			case <-ctx.Done():
				return nil
			}
		}
		// feed
		f.needsEntropy.UnSet()
		select {
		case rngFeeder <- f.buffer.CompileData():
		case <-ctx.Done():
			return nil
		}
		f.buffer = container.New()
	}
}
