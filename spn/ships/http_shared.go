package ships

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/spn/conf"
)

type sharedServer struct {
	server *http.Server

	handlers     map[string]http.HandlerFunc
	handlersLock sync.RWMutex
}

// ServeHTTP forwards requests to registered handler or uses defaults.
func (shared *sharedServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	shared.handlersLock.Lock()
	defer shared.handlersLock.Unlock()

	// Get and forward to registered handler.
	handler, ok := shared.handlers[r.URL.Path]
	if ok {
		handler(w, r)
		return
	}

	// If there is registered handler and path is "/", respond with info page.
	if r.Method == http.MethodGet && r.URL.Path == "/" {
		ServeInfoPage(w, r)
		return
	}

	// Otherwise, respond with error.
	http.Error(w, "", http.StatusNotFound)
}

var (
	sharedHTTPServers     = make(map[uint16]*sharedServer)
	sharedHTTPServersLock sync.Mutex
)

func addHTTPHandler(port uint16, path string, handler http.HandlerFunc) error {
	// Check params.
	if port == 0 {
		return errors.New("cannot listen on port 0")
	}

	// Default to root path.
	if path == "" {
		path = "/"
	}

	sharedHTTPServersLock.Lock()
	defer sharedHTTPServersLock.Unlock()

	// Get http server of the port.
	shared, ok := sharedHTTPServers[port]
	if ok {
		// Set path to handler.
		shared.handlersLock.Lock()
		defer shared.handlersLock.Unlock()

		// Check if path is already registered.
		_, ok := shared.handlers[path]
		if ok {
			return errors.New("path already registered")
		}

		// Else, register handler at path.
		shared.handlers[path] = handler
		return nil
	}

	// Shared server does not exist - create one.
	shared = &sharedServer{
		handlers: make(map[string]http.HandlerFunc),
	}

	// Add first handler.
	shared.handlers[path] = handler

	// Define new server.
	server := &http.Server{
		Addr:              fmt.Sprintf(":%d", port),
		Handler:           shared,
		ReadTimeout:       1 * time.Minute,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      1 * time.Minute,
		IdleTimeout:       1 * time.Minute,
		MaxHeaderBytes:    4096,
		BaseContext:       func(net.Listener) context.Context { return module.mgr.Ctx() },
	}
	shared.server = server

	// Start listeners.
	bindIPs := conf.GetBindIPs()
	listeners := make([]net.Listener, 0, len(bindIPs))
	for _, bindIP := range bindIPs {
		listener, err := net.ListenTCP("tcp", &net.TCPAddr{
			IP:   bindIP,
			Port: int(port),
		})
		if err != nil {
			return fmt.Errorf("failed to listen: %w", err)
		}

		listeners = append(listeners, listener)
		log.Infof("spn/ships: http transport pier established on %s", listener.Addr())
	}

	// Add shared http server to list.
	sharedHTTPServers[port] = shared

	// Start servers in service workers.
	for _, serviceListener := range listeners {
		module.mgr.Go(
			fmt.Sprintf("shared http server listener on %s", serviceListener.Addr()),
			func(_ *mgr.WorkerCtx) error {
				err := shared.server.Serve(serviceListener)
				if !errors.Is(http.ErrServerClosed, err) {
					return err
				}
				return nil
			},
		)
	}

	return nil
}

func removeHTTPHandler(port uint16, path string) error {
	// Check params.
	if port == 0 {
		return nil
	}

	// Default to root path.
	if path == "" {
		path = "/"
	}

	sharedHTTPServersLock.Lock()
	defer sharedHTTPServersLock.Unlock()

	// Get http server of the port.
	shared, ok := sharedHTTPServers[port]
	if !ok {
		return nil
	}

	// Set path to handler.
	shared.handlersLock.Lock()
	defer shared.handlersLock.Unlock()

	// Check if path is registered.
	_, ok = shared.handlers[path]
	if !ok {
		return nil
	}

	// Remove path from handler.
	delete(shared.handlers, path)

	// Shutdown shared HTTP server if no more handlers are registered.
	if len(shared.handlers) == 0 {
		ctx, cancel := context.WithTimeout(
			context.Background(),
			10*time.Second,
		)
		defer cancel()
		return shared.server.Shutdown(ctx)
	}

	// Remove shared HTTP server from map.
	delete(sharedHTTPServers, port)

	return nil
}
