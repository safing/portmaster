package ships

import (
	"net"

	"github.com/mr-tron/base58"
	"github.com/tevino/abool"

	"github.com/safing/portmaster/spn/hub"
)

// TestShip is a simulated ship that is used for testing higher level components.
type TestShip struct {
	mine      bool
	secure    bool
	loadSize  int
	forward   chan []byte
	backward  chan []byte
	unloadTmp []byte
	sinking   *abool.AtomicBool
}

// NewTestShip returns a new TestShip for simulation.
func NewTestShip(secure bool, loadSize int) *TestShip {
	return &TestShip{
		mine:     true,
		secure:   secure,
		loadSize: loadSize,
		forward:  make(chan []byte, 100),
		backward: make(chan []byte, 100),
		sinking:  abool.NewBool(false),
	}
}

// String returns a human readable informational summary about the ship.
func (ship *TestShip) String() string {
	if ship.mine {
		return "<TestShip outbound>"
	}
	return "<TestShip inbound>"
}

// Transport returns the transport used for this ship.
func (ship *TestShip) Transport() *hub.Transport {
	return &hub.Transport{
		Protocol: "dummy",
	}
}

// IsMine returns whether the ship was launched from here.
func (ship *TestShip) IsMine() bool {
	return ship.mine
}

// IsSecure returns whether the ship provides transport security.
func (ship *TestShip) IsSecure() bool {
	return ship.secure
}

// LoadSize returns the recommended data size that should be handed to Load().
// This value will be most likely somehow related to the connection's MTU.
// Alternatively, using a multiple of LoadSize is also recommended.
func (ship *TestShip) LoadSize() int {
	return ship.loadSize
}

// Reverse creates a connected TestShip. This is used to simulate a connection instead of using a Pier.
func (ship *TestShip) Reverse() *TestShip {
	return &TestShip{
		mine:     !ship.mine,
		secure:   ship.secure,
		loadSize: ship.loadSize,
		forward:  ship.backward,
		backward: ship.forward,
		sinking:  abool.NewBool(false),
	}
}

// Load loads data into the ship - ie. sends the data via the connection.
// Returns ErrSunk if the ship has already sunk earlier.
func (ship *TestShip) Load(data []byte) error {
	// Debugging:
	// log.Debugf("spn/ship: loading %s", spew.Sdump(data))

	// Check if ship is alive.
	if ship.sinking.IsSet() {
		return ErrSunk
	}

	// Empty load is used as a signal to cease operaetion.
	if len(data) == 0 {
		ship.Sink()
		return nil
	}

	// Send all given data.
	ship.forward <- data

	return nil
}

// UnloadTo unloads data from the ship - ie. receives data from the
// connection - puts it into the buf. It returns the amount of data
// written and an optional error.
// Returns ErrSunk if the ship has already sunk earlier.
func (ship *TestShip) UnloadTo(buf []byte) (n int, err error) {
	// Process unload tmp data, if there is any.
	if ship.unloadTmp != nil {
		// Copy as much data as possible.
		copy(buf, ship.unloadTmp)

		// If buf was too small, skip the copied section.
		if len(buf) < len(ship.unloadTmp) {
			ship.unloadTmp = ship.unloadTmp[len(buf):]
			return len(buf), nil
		}

		// If everything was copied, unset the unloadTmp data.
		n := len(ship.unloadTmp)
		ship.unloadTmp = nil
		return n, nil
	}

	// Receive data.
	data := <-ship.backward
	if len(data) == 0 {
		return 0, ErrSunk
	}

	// Copy data, possibly save remainder for later.
	copy(buf, data)
	if len(buf) < len(data) {
		ship.unloadTmp = data[len(buf):]
		return len(buf), nil
	}
	return len(data), nil
}

// Sink closes the underlying connection and cleans up any related resources.
func (ship *TestShip) Sink() {
	if ship.sinking.SetToIf(false, true) {
		close(ship.forward)
	}
}

// Dummy methods to conform to interface for testing.

func (ship *TestShip) LocalAddr() net.Addr              { return nil }                  //nolint:golint
func (ship *TestShip) RemoteAddr() net.Addr             { return nil }                  //nolint:golint
func (ship *TestShip) Public() bool                     { return true }                 //nolint:golint
func (ship *TestShip) MarkPublic()                      {}                              //nolint:golint
func (ship *TestShip) MaskAddress(addr net.Addr) string { return addr.String() }        //nolint:golint
func (ship *TestShip) MaskIP(ip net.IP) string          { return ip.String() }          //nolint:golint
func (ship *TestShip) Mask(value []byte) string         { return base58.Encode(value) } //nolint:golint
