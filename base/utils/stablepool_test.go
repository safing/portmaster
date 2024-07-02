package utils

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestStablePoolRealWorld(t *testing.T) {
	t.Parallel()
	// "real world" simulation

	cnt := 0
	testPool := &StablePool{
		New: func() interface{} {
			cnt++
			return cnt
		},
	}
	var testWg sync.WaitGroup
	var testWorkerWg sync.WaitGroup

	// for i := 0; i < 100; i++ {
	// 	cnt++
	// 	testPool.Put(cnt)
	// }
	for i := 0; i < 100; i++ {
		// block round
		testWg.Add(1)
		// add workers
		testWorkerWg.Add(100)
		for j := 0; j < 100; j++ {
			k := j
			go func() {
				// wait for round to start
				testWg.Wait()
				// get value
				x := testPool.Get()
				// fmt.Println(x)
				// "work"
				time.Sleep(5 * time.Microsecond)
				// re-insert 99%
				if k%100 > 0 {
					testPool.Put(x)
				}
				// mark as finished
				testWorkerWg.Done()
			}()
		}
		// start round
		testWg.Done()
		// wait for round to finish
		testWorkerWg.Wait()
	}
	t.Logf("real world simulation: cnt=%d p.cnt=%d p.max=%d\n", cnt, testPool.Size(), testPool.Max())
	assert.GreaterOrEqual(t, 200, cnt, "should not use more than 200 values")
	assert.GreaterOrEqual(t, 100, testPool.Max(), "pool should have at most this max size")

	// optimal usage test

	optPool := &StablePool{}
	for i := 0; i < 1000; i++ {
		for j := 0; j < 100; j++ {
			optPool.Put(j)
		}
		for k := 0; k < 100; k++ {
			assert.Equal(t, k, optPool.Get(), "should match")
		}
	}
	assert.Equal(t, 100, optPool.Max(), "pool should have exactly this max size")
}

func TestStablePoolFuzzing(t *testing.T) {
	t.Parallel()
	// fuzzing test

	fuzzPool := &StablePool{}
	var fuzzWg sync.WaitGroup
	var fuzzWorkerWg sync.WaitGroup
	// start goroutines and wait
	fuzzWg.Add(1)
	for i := 0; i < 1000; i++ {
		fuzzWorkerWg.Add(2)
		j := i
		go func() {
			fuzzWg.Wait()
			fuzzPool.Put(j)
			fuzzWorkerWg.Done()
		}()
		go func() {
			fuzzWg.Wait()
			fmt.Print(fuzzPool.Get())
			fuzzWorkerWg.Done()
		}()
	}
	// kick off
	fuzzWg.Done()
	// wait for all to finish
	fuzzWorkerWg.Wait()
}

func TestStablePoolBreaking(t *testing.T) {
	t.Parallel()
	// try to break it

	breakPool := &StablePool{}
	for i := 0; i < 10; i++ {
		for j := 0; j < 100; j++ {
			breakPool.Put(nil)
			breakPool.Put(j)
			breakPool.Put(nil)
		}
		for k := 0; k < 100; k++ {
			assert.Equal(t, k, breakPool.Get(), "should match")
		}
	}
}
