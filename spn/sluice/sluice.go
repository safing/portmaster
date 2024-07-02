package sluice

import (
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/netenv"
)

// Sluice is a tunnel entry listener.
type Sluice struct {
	network        string
	address        string
	createListener ListenerFactory

	lock            sync.Mutex
	listener        net.Listener
	pendingRequests map[string]*Request
	abandoned       bool
}

// ListenerFactory defines a function to create a listener.
type ListenerFactory func(network, address string) (net.Listener, error)

// StartSluice starts a sluice listener at the given address.
func StartSluice(network, address string) {
	s := &Sluice{
		network:         network,
		address:         address,
		pendingRequests: make(map[string]*Request),
	}

	switch s.network {
	case "tcp4", "tcp6":
		s.createListener = net.Listen
	case "udp4", "udp6":
		s.createListener = ListenUDP
	default:
		log.Errorf("spn/sluice: cannot start sluice for %s: unsupported network", network)
		return
	}

	// Start service worker.
	module.mgr.Go(
		s.network+" sluice listener",
		s.listenHandler,
	)
}

// AwaitRequest pre-registers a connection.
func (s *Sluice) AwaitRequest(r *Request) error {
	// Set default expiry.
	if r.Expires.IsZero() {
		r.Expires = time.Now().Add(defaultSluiceTTL)
	}

	s.lock.Lock()
	defer s.lock.Unlock()

	// Check if a pending request already exists for this local address.
	key := net.JoinHostPort(r.ConnInfo.LocalIP.String(), strconv.Itoa(int(r.ConnInfo.LocalPort)))
	_, exists := s.pendingRequests[key]
	if exists {
		return fmt.Errorf("a pending request for %s already exists", key)
	}

	// Add to pending requests.
	s.pendingRequests[key] = r
	return nil
}

func (s *Sluice) getRequest(address string) (r *Request, ok bool) {
	s.lock.Lock()
	defer s.lock.Unlock()

	r, ok = s.pendingRequests[address]
	if ok {
		delete(s.pendingRequests, address)
	}
	return
}

func (s *Sluice) init() error {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.abandoned = false

	// start listening
	s.listener = nil
	ln, err := s.createListener(s.network, s.address)
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}
	s.listener = ln

	// Add to registry.
	addSluice(s)

	return nil
}

func (s *Sluice) abandon() {
	s.lock.Lock()
	defer s.lock.Unlock()
	if s.abandoned {
		return
	}
	s.abandoned = true

	// Remove from registry.
	removeSluice(s.network)

	// Close listener.
	if s.listener != nil {
		_ = s.listener.Close()
	}

	// Notify pending requests.
	for i, r := range s.pendingRequests {
		r.CallbackFn(r.ConnInfo, nil)
		delete(s.pendingRequests, i)
	}
}

func (s *Sluice) handleConnection(conn net.Conn) {
	// Close the connection if handling is not successful.
	success := false
	defer func() {
		if !success {
			_ = conn.Close()
		}
	}()

	// Get IP address.
	var remoteIP net.IP
	switch typedAddr := conn.RemoteAddr().(type) {
	case *net.TCPAddr:
		remoteIP = typedAddr.IP
	case *net.UDPAddr:
		remoteIP = typedAddr.IP
	default:
		log.Warningf("spn/sluice: cannot handle connection for unsupported network %s", conn.RemoteAddr().Network())
		return
	}

	// Check if the request is local.
	local, err := netenv.IsMyIP(remoteIP)
	if err != nil {
		log.Warningf("spn/sluice: failed to check if request from %s is local: %s", remoteIP, err)
		return
	}
	if !local {
		log.Warningf("spn/sluice: received external request from %s, ignoring", remoteIP)

		// TODO:
		// Do not allow this to be spammed.
		// Only allow one trigger per second.
		// Do not trigger by same "remote IP" in a row.
		netenv.TriggerNetworkChangeCheck()

		return
	}

	// Get waiting request.
	r, ok := s.getRequest(conn.RemoteAddr().String())
	if !ok {
		_, err := conn.Write(entrypointInfoMsg)
		if err != nil {
			log.Warningf("spn/sluice: new %s request from %s without pending request, but failed to reply with info msg: %s", s.network, conn.RemoteAddr(), err)
		} else {
			log.Debugf("spn/sluice: new %s request from %s without pending request, replied with info msg", s.network, conn.RemoteAddr())
		}
		return
	}

	// Hand over to callback.
	log.Tracef(
		"spn/sluice: new %s request from %s for %s (%s:%d)",
		s.network, conn.RemoteAddr(),
		r.ConnInfo.Entity.Domain, r.ConnInfo.Entity.IP, r.ConnInfo.Entity.Port,
	)
	r.CallbackFn(r.ConnInfo, conn)
	success = true
}

func (s *Sluice) listenHandler(_ *mgr.WorkerCtx) error {
	defer s.abandon()
	err := s.init()
	if err != nil {
		return err
	}

	// Handle new connections.
	log.Infof("spn/sluice: started listening for %s requests on %s", s.network, s.listener.Addr())
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			if module.mgr.IsDone() {
				return nil
			}
			return fmt.Errorf("failed to accept connection: %w", err)
		}

		// Handle accepted connection.
		s.handleConnection(conn)

		// Clean up old leftovers.
		s.cleanConnections()
	}
}

func (s *Sluice) cleanConnections() {
	s.lock.Lock()
	defer s.lock.Unlock()

	now := time.Now()
	for address, request := range s.pendingRequests {
		if now.After(request.Expires) {
			delete(s.pendingRequests, address)
			log.Debugf("spn/sluice: removed expired pending %s connection %s", s.network, request.ConnInfo)
		}
	}
}
