package api

import (
	"errors"
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
