package runtime

import (
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/safing/portmaster/base/database/query"
	"github.com/safing/portmaster/base/database/record"
)

type testRecord struct {
	record.Base
	sync.Mutex
	Value string
}

func makeTestRecord(key, value string) record.Record {
	r := &testRecord{Value: value}
	r.CreateMeta()
	r.SetKey("runtime:" + key)
	return r
}

type testProvider struct {
	k string
	r []record.Record
}

func (tp *testProvider) Get(key string) ([]record.Record, error) {
	return tp.r, nil
}

func (tp *testProvider) Set(r record.Record) (record.Record, error) {
	return nil, errors.New("not implemented")
}

func getTestRegistry(t *testing.T) *Registry {
	t.Helper()

	r := NewRegistry()

	providers := []testProvider{
		{
			k: "p1/",
			r: []record.Record{
				makeTestRecord("p1/f1/v1", "p1.1"),
				makeTestRecord("p1/f2/v2", "p1.2"),
				makeTestRecord("p1/v3", "p1.3"),
			},
		},
		{
			k: "p2/f1",
			r: []record.Record{
				makeTestRecord("p2/f1/v1", "p2.1"),
				makeTestRecord("p2/f1/f2/v2", "p2.2"),
				makeTestRecord("p2/f1/v3", "p2.3"),
			},
		},
	}

	for idx := range providers {
		p := providers[idx]
		_, err := r.Register(p.k, &p)
		require.NoError(t, err)
	}

	return r
}

func TestRegistryGet(t *testing.T) {
	t.Parallel()

	var (
		r   record.Record
		err error
	)

	reg := getTestRegistry(t)

	r, err = reg.Get("p1/f1/v1")
	require.NoError(t, err)
	require.NotNil(t, r)
	assert.Equal(t, "p1.1", r.(*testRecord).Value) //nolint:forcetypeassert

	r, err = reg.Get("p1/v3")
	require.NoError(t, err)
	require.NotNil(t, r)
	assert.Equal(t, "p1.3", r.(*testRecord).Value) //nolint:forcetypeassert

	r, err = reg.Get("p1/v4")
	require.Error(t, err)
	assert.Nil(t, r)

	r, err = reg.Get("no-provider/foo")
	require.Error(t, err)
	assert.Nil(t, r)
}

func TestRegistryQuery(t *testing.T) {
	t.Parallel()

	reg := getTestRegistry(t)

	q := query.New("runtime:p")
	iter, err := reg.Query(q, true, true)
	require.NoError(t, err)
	require.NotNil(t, iter)
	var records []record.Record //nolint:prealloc
	for r := range iter.Next {
		records = append(records, r)
	}
	assert.Len(t, records, 6)

	q = query.New("runtime:p1/f")
	iter, err = reg.Query(q, true, true)
	require.NoError(t, err)
	require.NotNil(t, iter)
	records = nil
	for r := range iter.Next {
		records = append(records, r)
	}
	assert.Len(t, records, 2)
}

func TestRegistryRegister(t *testing.T) {
	t.Parallel()

	r := NewRegistry()

	cases := []struct {
		inp string
		err bool
	}{
		{"runtime:foo/bar/bar", false},
		{"runtime:foo/bar/bar2", false},
		{"runtime:foo/bar", false},
		{"runtime:foo/bar", true},  // already used
		{"runtime:foo/bar/", true}, // cannot register a prefix if there are providers below
		{"runtime:foo/baz/", false},
		{"runtime:foo/baz2/", false},
		{"runtime:foo/baz3", false},
		{"runtime:foo/baz/bar", true},
	}

	for _, c := range cases {
		_, err := r.Register(c.inp, nil)
		if c.err {
			assert.Error(t, err, c.inp)
		} else {
			assert.NoError(t, err, c.inp)
		}
	}
}
