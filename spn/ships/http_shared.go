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
	server    *http.Server
	listeners []net.Listener

	handlers     map[string]http.HandlerFunc
	handlersLock sync.RWMutex
}

// ServeHTTP forwards requests to registered handler or uses defaults.
func (shared *sharedServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Get the handler, but do not hold the lock while it runs: handlers may
	// take long, and requests must not wait for each other.
	shared.handlersLock.RLock()
	handler, ok := shared.handlers[r.URL.Path]
	shared.handlersLock.RUnlock()

	// Forward to registered handler.
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
			// Close the listeners that were already opened.
			for _, l := range listeners {
				_ = l.Close()
			}
			return fmt.Errorf("failed to listen: %w", err)
		}

		listeners = append(listeners, listener)
		log.Infof("spn/ships: http transport pier established on %s", listener.Addr())
	}

	// Add shared http server to list.
	shared.listeners = listeners
	sharedHTTPServers[port] = shared

	// Start servers in service workers.
	for _, serviceListener := range listeners {
		module.mgr.Go(
			fmt.Sprintf("shared http server listener on %s", serviceListener.Addr()),
			func(_ *mgr.WorkerCtx) error {
				err := shared.server.Serve(serviceListener)
				if !errors.Is(err, http.ErrServerClosed) {
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

	shared := detachHTTPHandler(port, path)
	if shared == nil {
		return nil
	}

	// Shut the server down after releasing the locks, as shutting down waits
	// for active requests, which need the handlers lock.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := shared.server.Shutdown(ctx)

	// Shutdown only closes listeners the server already serves on, but the
	// listener workers may not have started yet. Close all of them, so that
	// the port is released now.
	for _, listener := range shared.listeners {
		_ = listener.Close()
	}
	return err
}

// detachHTTPHandler removes the handler at path from the shared server of
// port. If no more handlers are registered, it also removes the shared server
// and returns it, so that the caller can shut it down.
func detachHTTPHandler(port uint16, path string) *sharedServer {
	sharedHTTPServersLock.Lock()
	defer sharedHTTPServersLock.Unlock()

	// Get http server of the port.
	shared, ok := sharedHTTPServers[port]
	if !ok {
		return nil
	}

	shared.handlersLock.Lock()
	defer shared.handlersLock.Unlock()

	// Remove path from handlers.
	delete(shared.handlers, path)
	if len(shared.handlers) > 0 {
		return nil
	}

	// No more handlers are registered: remove the shared server.
	delete(sharedHTTPServers, port)
	return shared
}
