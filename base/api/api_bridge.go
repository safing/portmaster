package api

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path"
	"strings"
	"sync"

	"github.com/safing/portmaster/base/database"
	"github.com/safing/portmaster/base/database/record"
	"github.com/safing/portmaster/base/database/storage"
)

const (
	endpointBridgeRemoteAddress = "websocket-bridge"
	apiDatabaseName             = "api"
)

func registerEndpointBridgeDB() error {
	if _, err := database.Register(&database.Database{
		Name:        apiDatabaseName,
		Description: "API Bridge",
		StorageType: "injected",
	}); err != nil {
		return err
	}

	_, err := database.InjectDatabase("api", &endpointBridgeStorage{})
	return err
}

type endpointBridgeStorage struct {
	storage.InjectBase
}

// EndpointBridgeRequest holds a bridged request API request.
type EndpointBridgeRequest struct {
	record.Base
	sync.Mutex

	Method   string
	Path     string
	Query    map[string]string
	Data     []byte
	MimeType string
}

// EndpointBridgeResponse holds a bridged request API response.
type EndpointBridgeResponse struct {
	record.Base
	sync.Mutex

	MimeType string
	Body     string
}

// Get returns a database record.
func (ebs *endpointBridgeStorage) Get(key string) (record.Record, error) {
	if key == "" {
		return nil, database.ErrNotFound
	}

	return callAPI(&EndpointBridgeRequest{
		Method: http.MethodGet,
		Path:   key,
	})
}

// Get returns the metadata of a database record.
func (ebs *endpointBridgeStorage) GetMeta(key string) (*record.Meta, error) {
	// This interface is an API, always return a fresh copy.
	m := &record.Meta{}
	m.Update()
	return m, nil
}

// Put stores a record in the database.
func (ebs *endpointBridgeStorage) Put(r record.Record) (record.Record, error) {
	if r.DatabaseKey() == "" {
		return nil, database.ErrNotFound
	}

	// Prepare data.
	var ebr *EndpointBridgeRequest
	if r.IsWrapped() {
		// Only allocate a new struct, if we need it.
		ebr = &EndpointBridgeRequest{}
		err := record.Unwrap(r, ebr)
		if err != nil {
			return nil, err
		}
	} else {
		var ok bool
		ebr, ok = r.(*EndpointBridgeRequest)
		if !ok {
			return nil, fmt.Errorf("record not of type *EndpointBridgeRequest, but %T", r)
		}
	}

	// Override path with key to mitigate sneaky stuff.
	ebr.Path = r.DatabaseKey()
	return callAPI(ebr)
}

// ReadOnly returns whether the database is read only.
func (ebs *endpointBridgeStorage) ReadOnly() bool {
	return false
}

func callAPI(ebr *EndpointBridgeRequest) (record.Record, error) {
	// Add API prefix to path.
	requestURL := path.Join(apiV1Path, ebr.Path)
	// Check if path is correct. (Defense in depth)
	if !strings.HasPrefix(requestURL, apiV1Path) {
		return nil, fmt.Errorf("bridged request for %q violates scope", ebr.Path)
	}

	// Apply default Method.
	if ebr.Method == "" {
		if len(ebr.Data) > 0 {
			ebr.Method = http.MethodPost
		} else {
			ebr.Method = http.MethodGet
		}
	}

	// Build URL.
	u, err := url.ParseRequestURI(requestURL)
	if err != nil {
		return nil, fmt.Errorf("failed to build bridged request url: %w", err)
	}
	// Build query values.
	if ebr.Query != nil && len(ebr.Query) > 0 {
		query := url.Values{}
		for k, v := range ebr.Query {
			query.Set(k, v)
		}
		u.RawQuery = query.Encode()
	}

	// Create request and response objects.
	r := httptest.NewRequest(ebr.Method, u.String(), bytes.NewBuffer(ebr.Data))
	r.RemoteAddr = endpointBridgeRemoteAddress
	if ebr.MimeType != "" {
		r.Header.Set("Content-Type", ebr.MimeType)
	}
	w := httptest.NewRecorder()
	// Let the API handle the request.
	server.Handler.ServeHTTP(w, r)
	switch w.Code {
	case 200:
		// Everything okay, continue.
	case 500:
		// A Go error was returned internally.
		// We can safely return this as an error.
		return nil, fmt.Errorf("bridged api call failed: %s", w.Body.String())
	default:
		return nil, fmt.Errorf("bridged api call returned unexpected error code %d", w.Code)
	}

	response := &EndpointBridgeResponse{
		MimeType: w.Header().Get("Content-Type"),
		Body:     w.Body.String(),
	}
	response.SetKey(apiDatabaseName + ":" + ebr.Path)
	response.UpdateMeta()

	return response, nil
}
