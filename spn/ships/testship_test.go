package ships

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTestShip(t *testing.T) {
	t.Parallel()

	tShip := NewTestShip(true, 100)

	// interface conformance test
	var ship Ship = tShip

	srvShip := tShip.Reverse()

	for range 100 {
		// client send
		err := ship.Load(testData)
		if err != nil {
			t.Fatalf("%s failed: %s", ship, err)
		}

		// server recv
		buf := getTestBuf()
		_, err = srvShip.UnloadTo(buf)
		if err != nil {
			t.Fatalf("%s failed: %s", ship, err)
		}

		// check data
		assert.Equal(t, testData, buf, "should match")
		fmt.Print(".")

		// server send
		err = srvShip.Load(testData)
		if err != nil {
			t.Fatalf("%s failed: %s", ship, err)
		}

		// client recv
		buf = getTestBuf()
		_, err = ship.UnloadTo(buf)
		if err != nil {
			t.Fatalf("%s failed: %s", ship, err)
		}

		// check data
		assert.Equal(t, testData, buf, "should match")
		fmt.Print(".")
	}

	ship.Sink()
	srvShip.Sink()
}
