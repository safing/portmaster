package ships

import (
	"context"
	"fmt"
	"net"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/safing/portmaster/spn/hub"
)

var (
	testPort  uint16 = 65000
	testData         = []byte("The quick brown fox jumps over the lazy dog")
	localhost        = net.IPv4(127, 0, 0, 1)
)

func getTestPort() uint16 {
	testPort++
	return testPort
}

func getTestBuf() []byte {
	return make([]byte, len(testData))
}

func TestConnections(t *testing.T) {
	t.Parallel()

	registryLock.Lock()
	t.Cleanup(func() {
		registryLock.Unlock()
	})

	for protocol, builder := range registry {
		t.Run(protocol, func(t *testing.T) {
			t.Parallel()

			var wg sync.WaitGroup
			ctx, cancelCtx := context.WithCancel(context.Background())

			// docking requests
			dockingRequests := make(chan Ship, 1)
			transport := &hub.Transport{
				Protocol: protocol,
				Port:     getTestPort(),
			}

			// create listener
			pier, err := builder.EstablishPier(transport, dockingRequests)
			if err != nil {
				t.Fatal(err)
			}

			// connect to listener
			ship, err := builder.LaunchShip(ctx, transport, localhost)
			if err != nil {
				t.Fatal(err)
			}

			// client send
			err = ship.Load(testData)
			if err != nil {
				t.Fatalf("%s failed: %s", ship, err)
			}

			// dock client
			srvShip := <-dockingRequests
			if srvShip == nil {
				t.Fatalf("%s failed to dock", pier)
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

			for range 100 {
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

				// client send
				err = ship.Load(testData)
				if err != nil {
					t.Fatalf("%s failed: %s", ship, err)
				}

				// server recv
				buf = getTestBuf()
				_, err = srvShip.UnloadTo(buf)
				if err != nil {
					t.Fatalf("%s failed: %s", ship, err)
				}

				// check data
				assert.Equal(t, testData, buf, "should match")
				fmt.Print(".")
			}

			ship.Sink()
			srvShip.Sink()
			pier.Abolish()
			cancelCtx()
			wg.Wait() // wait for docking procedure to end
		})
	}
}
