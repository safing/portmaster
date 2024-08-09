package api

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/gorilla/mux"

	"github.com/safing/portmaster/base/database/record"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/structures/dsd"
)

// Endpoint describes an API Endpoint.
// Path and at least one permission are required.
// As is exactly one function.
type Endpoint struct { //nolint:maligned
	// Name is the human reabable name of the endpoint.
	Name string
	// Description is the human readable description and documentation of the endpoint.
	Description string
	// Parameters is the parameter documentation.
	Parameters []Parameter `json:",omitempty"`

	// Path describes the URL path of the endpoint.
	Path string

	// MimeType defines the content type of the returned data.
	MimeType string

	// Read defines the required read permission.
	Read Permission `json:",omitempty"`

	// ReadMethod sets the required read method for the endpoint.
	// Available methods are:
	// GET: Returns data only, no action is taken, nothing is changed.
	// If omitted, defaults to GET.
	//
	// This field is currently being introduced and will only warn and not deny
	// access if the write method does not match.
	ReadMethod string `json:",omitempty"`

	// Write defines the required write permission.
	Write Permission `json:",omitempty"`

	// WriteMethod sets the required write method for the endpoint.
	// Available methods are:
	// POST: Create a new resource; Change a status; Execute a function
	// PUT: Update an existing resource
	// DELETE: Remove an existing resource
	// If omitted, defaults to POST.
	//
	// This field is currently being introduced and will only warn and not deny
	// access if the write method does not match.
	WriteMethod string `json:",omitempty"`

	// ActionFunc is for simple actions with a return message for the user.
	ActionFunc ActionFunc `json:"-"`

	// DataFunc is for returning raw data that the caller for further processing.
	DataFunc DataFunc `json:"-"`

	// StructFunc is for returning any kind of struct.
	StructFunc StructFunc `json:"-"`

	// RecordFunc is for returning a database record. It will be properly locked
	// and marshalled including metadata.
	RecordFunc RecordFunc `json:"-"`

	// HandlerFunc is the raw http handler.
	HandlerFunc http.HandlerFunc `json:"-"`
}

// Parameter describes a parameterized variation of an endpoint.
type Parameter struct {
	Method      string
	Field       string
	Value       string
	Description string
}

// HTTPStatusProvider is an interface for errors to provide a custom HTTP
// status code.
type HTTPStatusProvider interface {
	HTTPStatus() int
}

// HTTPStatusError represents an error with an HTTP status code.
type HTTPStatusError struct {
	err  error
	code int
}

// Error returns the error message.
func (e *HTTPStatusError) Error() string {
	return e.err.Error()
}

// Unwrap return the wrapped error.
func (e *HTTPStatusError) Unwrap() error {
	return e.err
}

// HTTPStatus returns the HTTP status code this error.
func (e *HTTPStatusError) HTTPStatus() int {
	return e.code
}

// ErrorWithStatus adds the HTTP status code to the error.
func ErrorWithStatus(err error, code int) error {
	return &HTTPStatusError{
		err:  err,
		code: code,
	}
}

type (
	// ActionFunc is for simple actions with a return message for the user.
	ActionFunc func(ar *Request) (msg string, err error)

	// DataFunc is for returning raw data that the caller for further processing.
	DataFunc func(ar *Request) (data []byte, err error)

	// StructFunc is for returning any kind of struct.
	StructFunc func(ar *Request) (i interface{}, err error)

	// RecordFunc is for returning a database record. It will be properly locked
	// and marshalled including metadata.
	RecordFunc func(ar *Request) (r record.Record, err error)
)

// MIME Types.
const (
	MimeTypeJSON string = "application/json"
	MimeTypeText string = "text/plain"

	apiV1Path = "/api/v1/"
)

func init() {
	RegisterHandler(apiV1Path+"{endpointPath:.+}", &endpointHandler{})
}

var (
	endpoints     = make(map[string]*Endpoint)
	endpointsMux  = mux.NewRouter()
	endpointsLock sync.RWMutex

	// ErrInvalidEndpoint is returned when an invalid endpoint is registered.
	ErrInvalidEndpoint = errors.New("endpoint is invalid")

	// ErrAlreadyRegistered is returned when there already is an endpoint with
	// the same path registered.
	ErrAlreadyRegistered = errors.New("an endpoint for this path is already registered")
)

func getAPIContext(r *http.Request) (apiEndpoint *Endpoint, apiRequest *Request) {
	// Get request context and check if we already have an action cached.
	apiRequest = GetAPIRequest(r)
	if apiRequest == nil {
		return nil, nil
	}
	var ok bool
	apiEndpoint, ok = apiRequest.HandlerCache.(*Endpoint)
	if ok {
		return apiEndpoint, apiRequest
	}

	endpointsLock.RLock()
	defer endpointsLock.RUnlock()

	// Get handler for request.
	// Gorilla does not support handling this on our own very well.
	// See github.com/gorilla/mux.ServeHTTP for reference.
	var match mux.RouteMatch
	var handler http.Handler
	if endpointsMux.Match(r, &match) {
		handler = match.Handler
		apiRequest.Route = match.Route
		// Add/Override variables instead of replacing.
		for k, v := range match.Vars {
			apiRequest.URLVars[k] = v
		}
	} else {
		return nil, apiRequest
	}

	apiEndpoint, ok = handler.(*Endpoint)
	if ok {
		// Cache for next operation.
		apiRequest.HandlerCache = apiEndpoint
	}
	return apiEndpoint, apiRequest
}

// RegisterEndpoint registers a new endpoint. An error will be returned if it
// does not pass the sanity checks.
func RegisterEndpoint(e Endpoint) error {
	if err := e.check(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidEndpoint, err)
	}

	endpointsLock.Lock()
	defer endpointsLock.Unlock()

	_, ok := endpoints[e.Path]
	if ok {
		return ErrAlreadyRegistered
	}

	endpoints[e.Path] = &e
	endpointsMux.Handle(apiV1Path+e.Path, &e)
	return nil
}

// GetEndpointByPath returns the endpoint registered with the given path.
func GetEndpointByPath(path string) (*Endpoint, error) {
	endpointsLock.Lock()
	defer endpointsLock.Unlock()
	endpoint, ok := endpoints[path]
	if !ok {
		return nil, fmt.Errorf("no registered endpoint on path: %q", path)
	}

	return endpoint, nil
}

func (e *Endpoint) check() error {
	// Check path.
	if strings.TrimSpace(e.Path) == "" {
		return errors.New("path is missing")
	}

	// Check permissions.
	if e.Read < Dynamic || e.Read > PermitSelf {
		return errors.New("invalid read permission")
	}
	if e.Write < Dynamic || e.Write > PermitSelf {
		return errors.New("invalid write permission")
	}

	// Check methods.
	if e.Read != NotSupported {
		switch e.ReadMethod {
		case http.MethodGet:
			// All good.
		case "":
			// Set to default.
			e.ReadMethod = http.MethodGet
		default:
			return errors.New("invalid read method")
		}
	} else {
		e.ReadMethod = ""
	}
	if e.Write != NotSupported {
		switch e.WriteMethod {
		case http.MethodPost,
			http.MethodPut,
			http.MethodDelete:
			// All good.
		case "":
			// Set to default.
			e.WriteMethod = http.MethodPost
		default:
			return errors.New("invalid write method")
		}
	} else {
		e.WriteMethod = ""
	}

	// Check functions.
	var defaultMimeType string
	fnCnt := 0
	if e.ActionFunc != nil {
		fnCnt++
		defaultMimeType = MimeTypeText
	}
	if e.DataFunc != nil {
		fnCnt++
		defaultMimeType = MimeTypeText
	}
	if e.StructFunc != nil {
		fnCnt++
		defaultMimeType = MimeTypeJSON
	}
	if e.RecordFunc != nil {
		fnCnt++
		defaultMimeType = MimeTypeJSON
	}
	if e.HandlerFunc != nil {
		fnCnt++
		defaultMimeType = MimeTypeText
	}
	if fnCnt != 1 {
		return errors.New("only one function may be set")
	}

	// Set default mime type.
	if e.MimeType == "" {
		e.MimeType = defaultMimeType
	}

	return nil
}

// ExportEndpoints exports the registered endpoints. The returned data must be
// treated as immutable.
func ExportEndpoints() []*Endpoint {
	endpointsLock.RLock()
	defer endpointsLock.RUnlock()

	// Copy the map into a slice.
	eps := make([]*Endpoint, 0, len(endpoints))
	for _, ep := range endpoints {
		eps = append(eps, ep)
	}

	sort.Sort(sortByPath(eps))
	return eps
}

type sortByPath []*Endpoint

func (eps sortByPath) Len() int           { return len(eps) }
func (eps sortByPath) Less(i, j int) bool { return eps[i].Path < eps[j].Path }
func (eps sortByPath) Swap(i, j int)      { eps[i], eps[j] = eps[j], eps[i] }

type endpointHandler struct{}

var _ AuthenticatedHandler = &endpointHandler{} // Compile time interface check.

// ReadPermission returns the read permission for the handler.
func (eh *endpointHandler) ReadPermission(r *http.Request) Permission {
	apiEndpoint, _ := getAPIContext(r)
	if apiEndpoint != nil {
		return apiEndpoint.Read
	}
	return NotFound
}

// WritePermission returns the write permission for the handler.
func (eh *endpointHandler) WritePermission(r *http.Request) Permission {
	apiEndpoint, _ := getAPIContext(r)
	if apiEndpoint != nil {
		return apiEndpoint.Write
	}
	return NotFound
}

// ServeHTTP handles the http request.
func (eh *endpointHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	apiEndpoint, apiRequest := getAPIContext(r)
	if apiEndpoint == nil || apiRequest == nil {
		http.NotFound(w, r)
		return
	}

	apiEndpoint.ServeHTTP(w, r)
}

// ServeHTTP handles the http request.
func (e *Endpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	_, apiRequest := getAPIContext(r)
	if apiRequest == nil {
		http.NotFound(w, r)
		return
	}

	// Return OPTIONS request before starting to handle normal requests.
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	eMethod, readMethod, ok := getEffectiveMethod(r)
	if !ok {
		http.Error(w, "unsupported method for the actions API", http.StatusMethodNotAllowed)
		return
	}

	if readMethod {
		if eMethod != e.ReadMethod {
			log.Tracer(r.Context()).Warningf(
				"api: method %q does not match required read method %q%s",
				r.Method,
				e.ReadMethod,
				" - this will be an error and abort the request in the future",
			)
		}
	} else {
		if eMethod != e.WriteMethod {
			log.Tracer(r.Context()).Warningf(
				"api: method %q does not match required write method %q%s",
				r.Method,
				e.WriteMethod,
				" - this will be an error and abort the request in the future",
			)
		}
	}

	switch eMethod {
	case http.MethodGet, http.MethodDelete:
		// Nothing to do for these.
	case http.MethodPost, http.MethodPut:
		// Read body data.
		inputData, ok := readBody(w, r)
		if !ok {
			return
		}
		apiRequest.InputData = inputData

		// restore request body for any http.HandlerFunc below
		r.Body = io.NopCloser(bytes.NewReader(inputData))
	default:
		// Defensive.
		http.Error(w, "unsupported method for the actions API", http.StatusMethodNotAllowed)
		return
	}

	// Add response headers to request struct so that the endpoint can work with them.
	apiRequest.ResponseHeader = w.Header()

	// Execute action function and get response data
	var responseData []byte
	var err error

	switch {
	case e.ActionFunc != nil:
		var msg string
		msg, err = e.ActionFunc(apiRequest)
		if !strings.HasSuffix(msg, "\n") {
			msg += "\n"
		}
		if err == nil {
			responseData = []byte(msg)
		}

	case e.DataFunc != nil:
		responseData, err = e.DataFunc(apiRequest)

	case e.StructFunc != nil:
		var v interface{}
		v, err = e.StructFunc(apiRequest)
		if err == nil && v != nil {
			var mimeType string
			responseData, mimeType, _, err = dsd.MimeDump(v, r.Header.Get("Accept"))
			if err == nil {
				w.Header().Set("Content-Type", mimeType)
			}
		}

	case e.RecordFunc != nil:
		var rec record.Record
		rec, err = e.RecordFunc(apiRequest)
		if err == nil && r != nil {
			responseData, err = MarshalRecord(rec, false)
		}

	case e.HandlerFunc != nil:
		e.HandlerFunc(w, r)
		return

	default:
		http.Error(w, "missing handler", http.StatusInternalServerError)
		return
	}

	// Check for handler error.
	if err != nil {
		var statusProvider HTTPStatusProvider
		if errors.As(err, &statusProvider) {
			http.Error(w, err.Error(), statusProvider.HTTPStatus())
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	// Return no content if there is none, or if request is HEAD.
	if len(responseData) == 0 || r.Method == http.MethodHead {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// Set content type if not yet set.
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", e.MimeType+"; charset=utf-8")
	}

	// Write response.
	w.Header().Set("Content-Length", strconv.Itoa(len(responseData)))
	w.WriteHeader(http.StatusOK)
	_, err = w.Write(responseData)
	if err != nil {
		log.Tracer(r.Context()).Warningf("api: failed to write response: %s", err)
	}
}

func readBody(w http.ResponseWriter, r *http.Request) (inputData []byte, ok bool) {
	// Check for too long content in order to prevent death.
	if r.ContentLength > 20000000 { // 20MB
		http.Error(w, "too much input data", http.StatusRequestEntityTooLarge)
		return nil, false
	}

	// Read and close body.
	inputData, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body"+err.Error(), http.StatusInternalServerError)
		return nil, false
	}
	return inputData, true
}
