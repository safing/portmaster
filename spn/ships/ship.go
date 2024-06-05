package ships

import (
	"errors"
	"fmt"
	"net"

	"github.com/tevino/abool"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/spn/hub"
)

const (
	defaultLoadSize = 4096
)

// ErrSunk is returned when a ship sunk, ie. the connection was lost.
var ErrSunk = errors.New("ship sunk")

// Ship represents a network layer connection.
type Ship interface {
	// String returns a human readable informational summary about the ship.
	String() string

	// Transport returns the transport used for this ship.
	Transport() *hub.Transport

	// IsMine returns whether the ship was launched from here.
	IsMine() bool

	// IsSecure returns whether the ship provides transport security.
	IsSecure() bool

	// Public returns whether the ship is marked as public.
	Public() bool

	// MarkPublic marks the ship as public.
	MarkPublic()

	// LoadSize returns the recommended data size that should be handed to Load().
	// This value will be most likely somehow related to the connection's MTU.
	// Alternatively, using a multiple of LoadSize is also recommended.
	LoadSize() int

	// Load loads data into the ship - ie. sends the data via the connection.
	// Returns ErrSunk if the ship has already sunk earlier.
	Load(data []byte) error

	// UnloadTo unloads data from the ship - ie. receives data from the
	// connection - puts it into the buf. It returns the amount of data
	// written and an optional error.
	// Returns ErrSunk if the ship has already sunk earlier.
	UnloadTo(buf []byte) (n int, err error)

	// LocalAddr returns the underlying local net.Addr of the connection.
	LocalAddr() net.Addr

	// RemoteAddr returns the underlying remote net.Addr of the connection.
	RemoteAddr() net.Addr

	// Sink closes the underlying connection and cleans up any related resources.
	Sink()

	// MaskAddress masks the address, if enabled.
	MaskAddress(addr net.Addr) string
	// MaskIP masks an IP, if enabled.
	MaskIP(ip net.IP) string
	// Mask masks a value.
	Mask(value []byte) string
}

// ShipBase implements common functions to comply with the Ship interface.
type ShipBase struct {
	// conn is the actual underlying connection.
	conn net.Conn
	// transport holds the transport definition of the ship.
	transport *hub.Transport

	// mine specifies whether the ship was launched from here.
	mine bool
	// secure specifies whether the ship provides transport security.
	secure bool
	// public specifies whether the ship is public.
	public *abool.AtomicBool
	// bufSize specifies the size of the receive buffer.
	bufSize int
	// loadSize specifies the recommended data size that should be handed to Load().
	loadSize int

	// initial holds initial data from setting up the ship.
	initial []byte
	// sinking specifies if the connection is being closed.
	sinking *abool.AtomicBool
}

func (ship *ShipBase) initBase() {
	// init
	ship.sinking = abool.New()
	ship.public = abool.New()

	// set default
	if ship.loadSize == 0 {
		ship.loadSize = defaultLoadSize
	}
	if ship.bufSize == 0 {
		ship.bufSize = ship.loadSize
	}
}

// String returns a human readable informational summary about the ship.
func (ship *ShipBase) String() string {
	if ship.mine {
		return fmt.Sprintf("<Ship to %s using %s>", ship.MaskAddress(ship.RemoteAddr()), ship.transport)
	}
	return fmt.Sprintf("<Ship from %s using %s>", ship.MaskAddress(ship.RemoteAddr()), ship.transport)
}

// Transport returns the transport used for this ship.
func (ship *ShipBase) Transport() *hub.Transport {
	return ship.transport
}

// IsMine returns whether the ship was launched from here.
func (ship *ShipBase) IsMine() bool {
	return ship.mine
}

// IsSecure returns whether the ship provides transport security.
func (ship *ShipBase) IsSecure() bool {
	return ship.secure
}

// Public returns whether the ship is marked as public.
func (ship *ShipBase) Public() bool {
	return ship.public.IsSet()
}

// MarkPublic marks the ship as public.
func (ship *ShipBase) MarkPublic() {
	ship.public.Set()
}

// LoadSize returns the recommended data size that should be handed to Load().
// This value will be most likely somehow related to the connection's MTU.
// Alternatively, using a multiple of LoadSize is also recommended.
func (ship *ShipBase) LoadSize() int {
	return ship.loadSize
}

// Load loads data into the ship - ie. sends the data via the connection.
// Returns ErrSunk if the ship has already sunk earlier.
func (ship *ShipBase) Load(data []byte) error {
	// Empty load is used as a signal to cease operaetion.
	if len(data) == 0 {
		if ship.sinking.SetToIf(false, true) {
			_ = ship.conn.Close()
		}
		return nil
	}

	// Send all given data.
	n, err := ship.conn.Write(data)
	switch {
	case err != nil:
		return err
	case n == 0:
		return errors.New("loaded 0 bytes")
	case n < len(data):
		// If not all data was sent, try again.
		log.Debugf("spn/ships: %s only loaded %d/%d bytes", ship, n, len(data))
		data = data[n:]
		return ship.Load(data)
	}

	return nil
}

// UnloadTo unloads data from the ship - ie. receives data from the
// connection - puts it into the buf. It returns the amount of data
// written and an optional error.
// Returns ErrSunk if the ship has already sunk earlier.
func (ship *ShipBase) UnloadTo(buf []byte) (n int, err error) {
	// Process initial data, if there is any.
	if ship.initial != nil {
		// Copy as much data as possible.
		copy(buf, ship.initial)

		// If buf was too small, skip the copied section.
		if len(buf) < len(ship.initial) {
			ship.initial = ship.initial[len(buf):]
			return len(buf), nil
		}

		// If everything was copied, unset the initial data.
		n := len(ship.initial)
		ship.initial = nil
		return n, nil
	}

	// Receive data.
	return ship.conn.Read(buf)
}

// LocalAddr returns the underlying local net.Addr of the connection.
func (ship *ShipBase) LocalAddr() net.Addr {
	return ship.conn.LocalAddr()
}

// RemoteAddr returns the underlying remote net.Addr of the connection.
func (ship *ShipBase) RemoteAddr() net.Addr {
	return ship.conn.RemoteAddr()
}

// Sink closes the underlying connection and cleans up any related resources.
func (ship *ShipBase) Sink() {
	if ship.sinking.SetToIf(false, true) {
		_ = ship.conn.Close()
	}
}
