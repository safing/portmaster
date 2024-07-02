package api

import (
	"fmt"
	"net/http"

	"github.com/gorilla/mux"

	"github.com/safing/portmaster/base/log"
)

// Request is a support struct to pool more request related information.
type Request struct {
	// Request is the http request.
	*http.Request

	// InputData contains the request body for write operations.
	InputData []byte

	// Route of this request.
	Route *mux.Route

	// URLVars contains the URL variables extracted by the gorilla mux.
	URLVars map[string]string

	// AuthToken is the request-side authentication token assigned.
	AuthToken *AuthToken

	// ResponseHeader holds the response header.
	ResponseHeader http.Header

	// HandlerCache can be used by handlers to cache data between handlers within a request.
	HandlerCache interface{}
}

// apiRequestContextKey is a key used for the context key/value storage.
type apiRequestContextKey struct{}

// RequestContextKey is the key used to add the API request to the context.
var RequestContextKey = apiRequestContextKey{}

// GetAPIRequest returns the API Request of the given http request.
func GetAPIRequest(r *http.Request) *Request {
	ar, ok := r.Context().Value(RequestContextKey).(*Request)
	if ok {
		return ar
	}
	return nil
}

// TextResponse writes a text response.
func TextResponse(w http.ResponseWriter, r *http.Request, text string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, err := fmt.Fprintln(w, text)
	if err != nil {
		log.Tracer(r.Context()).Warningf("api: failed to write text response: %s", err)
	}
}
