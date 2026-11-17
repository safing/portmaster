package api

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/safing/portmaster/base/database/record"
)

const (
	successMsg = "endpoint api success"
	failedMsg  = "endpoint api failed"
)

type actionTestRecord struct {
	record.Base
	sync.Mutex
	Msg string
}

func TestEndpoints(t *testing.T) {
	t.Parallel()

	testHandler := &mainHandler{
		mux: mainMux,
	}

	// ActionFn

	assert.NoError(t, RegisterEndpoint(Endpoint{
		Path: "test/action",
		Read: PermitAnyone,
		ActionFunc: func(_ *Request) (msg string, err error) {
			return successMsg, nil
		},
	}))
	assert.HTTPBodyContains(t, testHandler.ServeHTTP, "GET", apiV1Path+"test/action", nil, successMsg)

	assert.NoError(t, RegisterEndpoint(Endpoint{
		Path: "test/action-err",
		Read: PermitAnyone,
		ActionFunc: func(_ *Request) (msg string, err error) {
			return "", errors.New(failedMsg)
		},
	}))
	assert.HTTPBodyContains(t, testHandler.ServeHTTP, "GET", apiV1Path+"test/action-err", nil, failedMsg)

	// DataFn

	assert.NoError(t, RegisterEndpoint(Endpoint{
		Path: "test/data",
		Read: PermitAnyone,
		DataFunc: func(_ *Request) (data []byte, err error) {
			return []byte(successMsg), nil
		},
	}))
	assert.HTTPBodyContains(t, testHandler.ServeHTTP, "GET", apiV1Path+"test/data", nil, successMsg)

	assert.NoError(t, RegisterEndpoint(Endpoint{
		Path: "test/data-err",
		Read: PermitAnyone,
		DataFunc: func(_ *Request) (data []byte, err error) {
			return nil, errors.New(failedMsg)
		},
	}))
	assert.HTTPBodyContains(t, testHandler.ServeHTTP, "GET", apiV1Path+"test/data-err", nil, failedMsg)

	// StructFn

	assert.NoError(t, RegisterEndpoint(Endpoint{
		Path: "test/struct",
		Read: PermitAnyone,
		StructFunc: func(_ *Request) (i interface{}, err error) {
			return &actionTestRecord{
				Msg: successMsg,
			}, nil
		},
	}))
	assert.HTTPBodyContains(t, testHandler.ServeHTTP, "GET", apiV1Path+"test/struct", nil, successMsg)

	assert.NoError(t, RegisterEndpoint(Endpoint{
		Path: "test/struct-err",
		Read: PermitAnyone,
		StructFunc: func(_ *Request) (i interface{}, err error) {
			return nil, errors.New(failedMsg)
		},
	}))
	assert.HTTPBodyContains(t, testHandler.ServeHTTP, "GET", apiV1Path+"test/struct-err", nil, failedMsg)

	// RecordFn

	assert.NoError(t, RegisterEndpoint(Endpoint{
		Path: "test/record",
		Read: PermitAnyone,
		RecordFunc: func(_ *Request) (r record.Record, err error) {
			r = &actionTestRecord{
				Msg: successMsg,
			}
			r.CreateMeta()
			return r, nil
		},
	}))
	assert.HTTPBodyContains(t, testHandler.ServeHTTP, "GET", apiV1Path+"test/record", nil, successMsg)

	assert.NoError(t, RegisterEndpoint(Endpoint{
		Path: "test/record-err",
		Read: PermitAnyone,
		RecordFunc: func(_ *Request) (r record.Record, err error) {
			return nil, errors.New(failedMsg)
		},
	}))
	assert.HTTPBodyContains(t, testHandler.ServeHTTP, "GET", apiV1Path+"test/record-err", nil, failedMsg)
}

// TestEndpointRecordFuncNilRecord checks that a RecordFunc returning neither
// a record nor an error results in an empty response, like the other handler
// types, instead of a panic.
func TestEndpointRecordFuncNilRecord(t *testing.T) {
	t.Parallel()

	testHandler := &mainHandler{
		mux: mainMux,
	}

	assert.NoError(t, RegisterEndpoint(Endpoint{
		Path: "test/record-nil",
		Read: PermitAnyone,
		RecordFunc: func(_ *Request) (r record.Record, err error) {
			return nil, nil
		},
	}))

	r := httptest.NewRequest(http.MethodGet, apiV1Path+"test/record-nil", nil)
	w := httptest.NewRecorder()
	testHandler.ServeHTTP(w, r)
	assert.Equal(t, http.StatusNoContent, w.Code, "body: %s", w.Body.String())
	assert.Empty(t, w.Body.String())
}

// zeroReader endlessly returns zero bytes.
type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

func TestEndpointBodyLimitWithoutContentLength(t *testing.T) {
	t.Parallel()

	testHandler := &mainHandler{
		mux: mainMux,
	}

	var receivedBytes int
	assert.NoError(t, RegisterEndpoint(Endpoint{
		Path:  "test/body-limit",
		Write: PermitAnyone,
		ActionFunc: func(ar *Request) (msg string, err error) {
			receivedBytes = len(ar.InputData)
			return successMsg, nil
		},
	}))

	// A body without a declared length (as with chunked transfer encoding)
	// must not bypass the body size limit.
	body := io.LimitReader(zeroReader{}, maxRequestBodySize+1)
	r := httptest.NewRequest(http.MethodPost, apiV1Path+"test/body-limit", body)
	r.ContentLength = -1
	w := httptest.NewRecorder()
	testHandler.ServeHTTP(w, r)
	assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code, "received %d bytes", receivedBytes)

	// A body at the limit is still accepted.
	body = io.LimitReader(zeroReader{}, maxRequestBodySize)
	r = httptest.NewRequest(http.MethodPost, apiV1Path+"test/body-limit", body)
	r.ContentLength = -1
	w = httptest.NewRecorder()
	testHandler.ServeHTTP(w, r)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, maxRequestBodySize, receivedBytes)
}

func TestActionRegistration(t *testing.T) {
	t.Parallel()

	assert.Error(t, RegisterEndpoint(Endpoint{}))

	assert.Error(t, RegisterEndpoint(Endpoint{
		Path: "test/err",
		Read: NotFound,
	}))
	assert.Error(t, RegisterEndpoint(Endpoint{
		Path: "test/err",
		Read: PermitSelf + 1,
	}))

	assert.Error(t, RegisterEndpoint(Endpoint{
		Path:  "test/err",
		Write: NotFound,
	}))
	assert.Error(t, RegisterEndpoint(Endpoint{
		Path:  "test/err",
		Write: PermitSelf + 1,
	}))

	assert.Error(t, RegisterEndpoint(Endpoint{
		Path: "test/err",
	}))

	assert.Error(t, RegisterEndpoint(Endpoint{
		Path: "test/err",
		ActionFunc: func(_ *Request) (msg string, err error) {
			return successMsg, nil
		},
		DataFunc: func(_ *Request) (data []byte, err error) {
			return []byte(successMsg), nil
		},
	}))

	assert.NoError(t, RegisterEndpoint(Endpoint{
		Path: "test/err",
		ActionFunc: func(_ *Request) (msg string, err error) {
			return successMsg, nil
		},
	}))
}
