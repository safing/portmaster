<<<<<<< HEAD
package rng

import (
	"testing"
)

func TestFullFeeder(t *testing.T) {
	t.Parallel()

	for i := 0; i < 10; i++ {
		go func() {
			rngFeeder <- []byte{0}
		}()
	}
}
||||||| 151a548c
=======
package rng

import (
	"testing"
)

func TestFullFeeder(t *testing.T) {
	t.Parallel()

	for range 10 {
		go func() {
			rngFeeder <- []byte{0}
		}()
	}
}
>>>>>>> develop
