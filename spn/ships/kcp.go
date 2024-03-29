package ships

// KCPShip is a ship that uses KCP.
type KCPShip struct {
	ShipBase
}

// KCPPier is a pier that uses KCP.
type KCPPier struct {
	PierBase
}

// TODO: Find a replacement for kcp, which turned out to not fit our use case.
/*
func init() {
	Register("kcp", &Builder{
		LaunchShip:    launchKCPShip,
		EstablishPier: establishKCPPier,
	})
}

func launchKCPShip(ctx context.Context, transport *hub.Transport, ip net.IP) (Ship, error) {
	conn, err := kcp.Dial(net.JoinHostPort(ip.String(), portToA(transport.Port)))
	if err != nil {
		return nil, err
	}

	ship := &KCPShip{
		ShipBase: ShipBase{
			conn:      conn,
			transport: transport,
			mine:      true,
			secure:    false,
			// Calculate KCP's MSS.
			loadSize: kcp.IKCP_MTU_DEF - kcp.IKCP_OVERHEAD,
		},
	}

	ship.initBase()
	return ship, nil
}

func establishKCPPier(transport *hub.Transport, dockingRequests chan *DockingRequest) (Pier, error) {
	listener, err := kcp.Listen(net.JoinHostPort("", portToA(transport.Port)))
	if err != nil {
		return nil, err
	}

	pier := &KCPPier{
		PierBase: PierBase{
			transport:       transport,
			listener:        listener,
			dockingRequests: dockingRequests,
		},
	}
	pier.PierBase.dockShip = pier.dockShip
	pier.initBase()
	return pier, nil
}

func (pier *KCPPier) dockShip() (Ship, error) {
	conn, err := pier.listener.Accept()
	if err != nil {
		return nil, err
	}

	ship := &KCPShip{
		ShipBase: ShipBase{
			conn:      conn,
			transport: pier.transport,
			mine:      false,
			secure:    false,
			// Calculate KCP's MSS.
			loadSize: kcp.IKCP_MTU_DEF - kcp.IKCP_OVERHEAD,
		},
	}

	ship.initBase()
	return ship, nil
}
*/
