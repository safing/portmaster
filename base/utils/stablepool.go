package utils

import "sync"

// A StablePool is a drop-in replacement for sync.Pool that is slower, but
// predictable.
// A StablePool is a set of temporary objects that may be individually saved and
// retrieved.
//
// In contrast to sync.Pool, items are not removed automatically. Every item
// will be returned at some point. Items are returned in a FIFO manner in order
// to evenly distribute usage of a set of items.
//
// A StablePool is safe for use by multiple goroutines simultaneously and must
// not be copied after first use.
type StablePool struct {
	lock sync.Mutex

	pool     []interface{}
	cnt      int
	getIndex int
	putIndex int

	// New optionally specifies a function to generate
	// a value when Get would otherwise return nil.
	// It may not be changed concurrently with calls to Get.
	New func() interface{}
}

// Put adds x to the pool.
func (p *StablePool) Put(x interface{}) {
	if x == nil {
		return
	}

	p.lock.Lock()
	defer p.lock.Unlock()

	// check if pool is full (or unitialized)
	if p.cnt == len(p.pool) {
		p.pool = append(p.pool, x)
		p.cnt++
		p.putIndex = p.cnt
		return
	}

	// correct putIndex
	p.putIndex %= len(p.pool)

	// iterate the whole pool once to find a free spot
	stopAt := p.putIndex - 1
	for i := p.putIndex; i != stopAt; i = (i + 1) % len(p.pool) {
		if p.pool[i] == nil {
			p.pool[i] = x
			p.cnt++
			p.putIndex = i + 1
			return
		}
	}
}

// Get returns the next item from the Pool, removes it from the Pool, and
// returns it to the caller.
// In contrast to sync.Pool, Get never ignores the pool.
// Callers should not assume any relation between values passed to Put and
// the values returned by Get.
//
// If Get would otherwise return nil and p.New is non-nil, Get returns
// the result of calling p.New.
func (p *StablePool) Get() interface{} {
	p.lock.Lock()
	defer p.lock.Unlock()

	// check if pool is empty
	if p.cnt == 0 {
		if p.New != nil {
			return p.New()
		}
		return nil
	}

	// correct getIndex
	p.getIndex %= len(p.pool)

	// iterate the whole pool to find an item
	stopAt := p.getIndex - 1
	for i := p.getIndex; i != stopAt; i = (i + 1) % len(p.pool) {
		if p.pool[i] != nil {
			x := p.pool[i]
			p.pool[i] = nil
			p.cnt--
			p.getIndex = i + 1
			return x
		}
	}

	// if we ever get here, return a new item
	if p.New != nil {
		return p.New()
	}
	return nil
}

// Size returns the amount of items the pool currently holds.
func (p *StablePool) Size() int {
	p.lock.Lock()
	defer p.lock.Unlock()

	return p.cnt
}

// Max returns the amount of items the pool held at maximum.
func (p *StablePool) Max() int {
	p.lock.Lock()
	defer p.lock.Unlock()

	return len(p.pool)
}
