package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/utils"
	"github.com/safing/portmaster/service/mgr"
)

// EnableServer defines if the HTTP server should be started.
var EnableServer = true

var (
	// mainMux is the main mux router.
	mainMux = mux.NewRouter()

	// server is the main server.
	server = &http.Server{
		ReadHeaderTimeout: 10 * time.Second,
	}
	handlerLock sync.RWMutex

	allowedDevCORSOrigins = []string{
		"127.0.0.1",
		"localhost",
	}
)

// RegisterHandler registers a handler with the API endpoint.
func RegisterHandler(path string, handler http.Handler) *mux.Route {
	handlerLock.Lock()
	defer handlerLock.Unlock()
	return mainMux.Handle(path, handler)
}

// RegisterHandleFunc registers a handle function with the API endpoint.
func RegisterHandleFunc(path string, handleFunc func(http.ResponseWriter, *http.Request)) *mux.Route {
	handlerLock.Lock()
	defer handlerLock.Unlock()
	return mainMux.HandleFunc(path, handleFunc)
}

func startServer() {
	// Check if server is enabled.
	if !EnableServer {
		return
	}

	// Configure server.
	server.Addr = listenAddressConfig()
	server.Handler = &mainHandler{
		// TODO: mainMux should not be modified anymore.
		mux: mainMux,
	}

	// Start server manager.
	module.mgr.Go("http server manager", serverManager)
}

func stopServer() error {
	// Check if server is enabled.
	if !EnableServer {
		return nil
	}

	if server.Addr != "" {
		return server.Shutdown(context.Background())
	}

	return nil
}

// Serve starts serving the API endpoint.
func serverManager(ctx *mgr.WorkerCtx) error {
	// start serving
	log.Infof("api: starting to listen on %s", server.Addr)
	backoffDuration := 10 * time.Second
	for {
		err := module.mgr.Do("http server", func(ctx *mgr.WorkerCtx) error {
			err := server.ListenAndServe()
			// return on shutdown error
			if errors.Is(err, http.ErrServerClosed) {
				return nil
			}
			return err
		})
		if err == nil {
			return nil
		}
		// log error and restart
		log.Errorf("api: http endpoint failed: %s - restarting in %s", err, backoffDuration)
		time.Sleep(backoffDuration)
	}
}

type mainHandler struct {
	mux *mux.Router
}

func (mh *mainHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	_ = module.mgr.Do("http request", func(_ *mgr.WorkerCtx) error {
		return mh.handle(w, r)
	})
}

func (mh *mainHandler) handle(w http.ResponseWriter, r *http.Request) error {
	// Setup context trace logging.
	ctx, tracer := log.AddTracer(r.Context())
	// Add request context.
	apiRequest := &Request{
		Request: r,
	}
	ctx = context.WithValue(ctx, RequestContextKey, apiRequest)
	// Add context back to request.
	r = r.WithContext(ctx)
	lrw := NewLoggingResponseWriter(w, r)

	tracer.Tracef("api request: %s ___ %s %s", r.RemoteAddr, lrw.Request.Method, r.RequestURI)
	defer func() {
		// Log request status.
		if lrw.Status != 0 {
			// If lrw.Status is 0, the request may have been hijacked.
			tracer.Debugf("api request: %s %d %s %s", lrw.Request.RemoteAddr, lrw.Status, lrw.Request.Method, lrw.Request.RequestURI)
		}
		tracer.Submit()
	}()

	// Add security headers.
	w.Header().Set("Referrer-Policy", "same-origin")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "deny")
	w.Header().Set("X-XSS-Protection", "1; mode=block")
	w.Header().Set("X-DNS-Prefetch-Control", "off")

	// Add CSP Header in production mode.
	if !devMode() {
		w.Header().Set(
			"Content-Security-Policy",
			"default-src 'self'; "+
				"connect-src https://*.safing.io 'self'; "+
				"style-src 'self' 'unsafe-inline'; "+
				"img-src 'self' data: blob:",
		)
	}

	// Check Cross-Origin Requests.
	origin := r.Header.Get("Origin")
	isPreflighCheck := false
	if origin != "" {

		// Parse origin URL.
		originURL, err := url.Parse(origin)
		if err != nil {
			tracer.Warningf("api: denied request from %s: failed to parse origin header: %s", r.RemoteAddr, err)
			http.Error(lrw, "Invalid Origin.", http.StatusForbidden)
			return nil
		}

		// Check if the Origin matches the Host.
		switch {
		case originURL.Host == r.Host:
			// Origin (with port) matches Host.
		case originURL.Hostname() == r.Host:
			// Origin (without port) matches Host.
		case originURL.Scheme == "chrome-extension":
			// Allow access for the browser extension
			// TODO(ppacher):
			// This currently allows access from any browser extension.
			// Can we reduce that to only our browser extension?
			// Also, what do we need to support Firefox?
		case devMode() &&
			utils.StringInSlice(allowedDevCORSOrigins, originURL.Hostname()):
			// We are in dev mode and the request is coming from the allowed
			// development origins.
		default:
			// Origin and Host do NOT match!
			tracer.Warningf("api: denied request from %s: Origin (`%s`) and Host (`%s`) do not match", r.RemoteAddr, origin, r.Host)
			http.Error(lrw, "Cross-Origin Request Denied.", http.StatusForbidden)
			return nil

			// If the Host header has a port, and the Origin does not, requests will
			// also end up here, as we cannot properly check for equality.
		}

		// Add Cross-Site Headers now as we need them in any case now.
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Methods", "*")
		w.Header().Set("Access-Control-Allow-Headers", "*")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Access-Control-Expose-Headers", "*")
		w.Header().Set("Access-Control-Max-Age", "60")
		w.Header().Add("Vary", "Origin")

		// if there's a Access-Control-Request-Method header this is a Preflight check.
		// In that case, we will just check if the preflighMethod is allowed and then return
		// success here
		if preflighMethod := r.Header.Get("Access-Control-Request-Method"); r.Method == http.MethodOptions && preflighMethod != "" {
			isPreflighCheck = true
		}
	}

	// Clean URL.
	cleanedRequestPath := cleanRequestPath(r.URL.Path)

	// If the cleaned URL differs from the original one, redirect to there.
	if r.URL.Path != cleanedRequestPath {
		redirURL := *r.URL
		redirURL.Path = cleanedRequestPath
		http.Redirect(lrw, r, redirURL.String(), http.StatusMovedPermanently)
		return nil
	}

	// Get handler for request.
	// Gorilla does not support handling this on our own very well.
	// See github.com/gorilla/mux.ServeHTTP for reference.
	var match mux.RouteMatch
	var handler http.Handler
	if mh.mux.Match(r, &match) {
		handler = match.Handler
		apiRequest.Route = match.Route
		apiRequest.URLVars = match.Vars
	}
	switch {
	case match.MatchErr == nil:
		// All good.
	case errors.Is(match.MatchErr, mux.ErrMethodMismatch):
		http.Error(lrw, "Method not allowed.", http.StatusMethodNotAllowed)
		return nil
	default:
		tracer.Debug("api: no handler registered for this path")
		http.Error(lrw, "Not found.", http.StatusNotFound)
		return nil
	}

	// Be sure that URLVars always is a map.
	if apiRequest.URLVars == nil {
		apiRequest.URLVars = make(map[string]string)
	}

	// Check method.
	_, readMethod, ok := getEffectiveMethod(r)
	if !ok {
		http.Error(lrw, "Method not allowed.", http.StatusMethodNotAllowed)
		return nil
	}

	// At this point we know the method is allowed and there's a handler for the request.
	// If this is just a CORS-Preflight, we'll accept the request with StatusOK now.
	// There's no point in trying to authenticate the request because the Browser will
	// not send authentication along a preflight check.
	if isPreflighCheck && handler != nil {
		lrw.WriteHeader(http.StatusOK)
		return nil
	}

	// Check authentication.
	apiRequest.AuthToken = authenticateRequest(lrw, r, handler, readMethod)
	if apiRequest.AuthToken == nil {
		// Authenticator already replied.
		return nil
	}

	// Check if we have a handler.
	if handler == nil {
		http.Error(lrw, "Not found.", http.StatusNotFound)
		return nil
	}

	// Format panics in handler.
	defer func() {
		if panicValue := recover(); panicValue != nil {
			// Log failure.
			log.Errorf("api: handler panic: %s", panicValue)
			// Respond with a server error.
			if devMode() {
				http.Error(
					lrw,
					fmt.Sprintf(
						"Internal Server Error: %s\n\n%s",
						panicValue,
						debug.Stack(),
					),
					http.StatusInternalServerError,
				)
			} else {
				http.Error(lrw, "Internal Server Error.", http.StatusInternalServerError)
			}
		}
	}()

	// Handle with registered handler.
	handler.ServeHTTP(lrw, r)

	return nil
}

// cleanRequestPath cleans and returns a request URL.
func cleanRequestPath(requestPath string) string {
	// If the request URL is empty, return a request for "root".
	if requestPath == "" || requestPath == "/" {
		return "/"
	}
	// If the request URL does not start with a slash, prepend it.
	if !strings.HasPrefix(requestPath, "/") {
		requestPath = "/" + requestPath
	}

	// Clean path to remove any relative parts.
	cleanedRequestPath := path.Clean(requestPath)
	// Because path.Clean removes a trailing slash, we need to add it back here
	// if the original URL had one.
	if strings.HasSuffix(requestPath, "/") {
		cleanedRequestPath += "/"
	}

	return cleanedRequestPath
}
