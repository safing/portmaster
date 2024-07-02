package ships

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/spn/conf"
	"github.com/safing/portmaster/spn/hub"
)

// TCPShip is a ship that uses TCP.
type TCPShip struct {
	ShipBase
}

// TCPPier is a pier that uses TCP.
type TCPPier struct {
	PierBase

	ctx       context.Context
	cancelCtx context.CancelFunc
}

func init() {
	Register("tcp", &Builder{
		LaunchShip:    launchTCPShip,
		EstablishPier: establishTCPPier,
	})
}

func launchTCPShip(ctx context.Context, transport *hub.Transport, ip net.IP) (Ship, error) {
	var dialNet string
	if ip4 := ip.To4(); ip4 != nil {
		dialNet = "tcp4"
	} else {
		dialNet = "tcp6"
	}
	dialer := &net.Dialer{
		Timeout:       30 * time.Second,
		LocalAddr:     conf.GetBindAddr(dialNet),
		FallbackDelay: -1, // Disables Fast Fallback from IPv6 to IPv4.
		KeepAlive:     -1, // Disable keep-alive.
	}
	conn, err := dialer.DialContext(ctx, dialNet, net.JoinHostPort(ip.String(), portToA(transport.Port)))
	if err != nil {
		return nil, fmt.Errorf("failed to connect: %w", err)
	}

	ship := &TCPShip{
		ShipBase: ShipBase{
			conn:      conn,
			transport: transport,
			mine:      true,
			secure:    false,
		},
	}

	ship.calculateLoadSize(ip, nil, TCPHeaderMTUSize)
	ship.initBase()
	return ship, nil
}

func establishTCPPier(transport *hub.Transport, dockingRequests chan Ship) (Pier, error) {
	// Start listeners.
	bindIPs := conf.GetBindIPs()
	listeners := make([]net.Listener, 0, len(bindIPs))
	for _, bindIP := range bindIPs {
		listener, err := net.ListenTCP("tcp", &net.TCPAddr{
			IP:   bindIP,
			Port: int(transport.Port),
		})
		if err != nil {
			return nil, fmt.Errorf("failed to listen: %w", err)
		}

		listeners = append(listeners, listener)
		log.Infof("spn/ships: tcp transport pier established on %s", listener.Addr())
	}

	// Create new pier.
	pierCtx, cancelCtx := context.WithCancel(module.mgr.Ctx())
	pier := &TCPPier{
		PierBase: PierBase{
			transport:       transport,
			listeners:       listeners,
			dockingRequests: dockingRequests,
		},
		ctx:       pierCtx,
		cancelCtx: cancelCtx,
	}
	pier.initBase()

	// Start workers.
	for _, listener := range pier.listeners {
		module.mgr.Go("accept TCP docking requests", func(wc *mgr.WorkerCtx) error {
			return pier.dockingWorker(wc.Ctx(), listener)
		})
	}

	return pier, nil
}

func (pier *TCPPier) dockingWorker(_ context.Context, listener net.Listener) error {
	for {
		// Block until something happens.
		conn, err := listener.Accept()

		// Check for errors.
		switch {
		case pier.ctx.Err() != nil:
			return pier.ctx.Err()
		case err != nil:
			return err
		}

		// Create new ship.
		ship := &TCPShip{
			ShipBase: ShipBase{
				transport: pier.transport,
				conn:      conn,
				mine:      false,
				secure:    false,
			},
		}
		ship.calculateLoadSize(nil, conn.RemoteAddr(), TCPHeaderMTUSize)
		ship.initBase()

		// Submit new docking request.
		select {
		case pier.dockingRequests <- ship:
		case <-pier.ctx.Done():
			return pier.ctx.Err()
		}
	}
}

// Abolish closes the underlying listener and cleans up any related resources.
func (pier *TCPPier) Abolish() {
	pier.cancelCtx()
	pier.PierBase.Abolish()
}
