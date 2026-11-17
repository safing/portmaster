package orm

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type encoderCacheTestRow struct {
	ID       string     `sqlite:"id,primary"`
	private  string     //nolint:unused // Must be ignored by the encoder.
	Count    uint16     `sqlite:"count"`
	Started  time.Time  `sqlite:"started,text,time"`
	Ended    *time.Time `sqlite:"ended,text,time"`
	Allowed  *bool      `sqlite:"allowed"`
	Name     *string    `sqlite:"name"`
	Untagged float64
	Skipped  uint64 `sqlite:"skipped,default=0"`
}

func TestToParamMapCachedFields(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	ended := time.Date(2022, time.February, 15, 10, 0, 0, 0, time.UTC)
	allowed := false
	full := encoderCacheTestRow{
		ID:       "a",
		Count:    7,
		Started:  time.Date(2022, time.February, 15, 9, 51, 0, 0, time.UTC),
		Ended:    &ended,
		Allowed:  &allowed,
		Untagged: 1.5,
		Skipped:  3,
	}

	// Encode several times, by value and by pointer, so that the cached
	// field metadata is used.
	for range 3 {
		for _, input := range []any{full, &full} {
			m, err := ToParamMap(ctx, input, ":", DefaultEncodeConfig, []string{"skipped"})
			require.NoError(t, err)
			assert.Equal(t, map[string]any{
				":id":       "a",
				":count":    uint16(7),
				":started":  "2022-02-15 09:51:00",
				":ended":    "2022-02-15 10:00:00",
				":allowed":  false,
				":name":     nil,
				":Untagged": 1.5,
			}, m)
		}
	}

	// Zero time and nil pointers.
	m, err := ToParamMap(ctx, encoderCacheTestRow{}, "", DefaultEncodeConfig, nil)
	require.NoError(t, err)
	assert.Equal(t, map[string]any{
		"id":       "",
		"count":    uint16(0),
		"started":  nil,
		"ended":    nil,
		"allowed":  nil,
		"name":     nil,
		"Untagged": 0.0,
		"skipped":  uint64(0),
	}, m)

	// A different type with the same field names must not use the cached
	// metadata of the first type.
	m, err = ToParamMap(ctx, struct {
		ID    int `sqlite:"other_id"`
		Count string
	}{ID: 1, Count: "x"}, "", DefaultEncodeConfig, nil)
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"other_id": 1, "Count": "x"}, m)
}

func TestToParamMapColumnDefError(t *testing.T) {
	t.Parallel()

	type withIgnored struct {
		A string
		B string `sqlite:"-"`
	}

	// Fields that cannot be described as a column fail the encoding, also
	// when the metadata comes from the cache.
	for range 2 {
		_, err := ToParamMap(context.Background(), withIgnored{}, "", DefaultEncodeConfig, nil)
		require.ErrorIs(t, err, errSkipStructField)
	}

	_, err := ToParamMap(context.Background(), "not a struct", "", DefaultEncodeConfig, nil)
	require.ErrorIs(t, err, errStructExpected)
}

func TestToParamMapDoesNotModifyHooks(t *testing.T) {
	t.Parallel()

	var calls int
	hook := func(*ColumnDef, reflect.Type, reflect.Value) (any, bool, error) {
		calls++
		return nil, false, nil
	}
	sentinel := func(*ColumnDef, reflect.Type, reflect.Value) (any, bool, error) {
		return "sentinel", true, nil
	}

	// Hooks with spare capacity: the basic encoder must not be written into
	// the backing array of the caller.
	backing := []EncodeFunc{hook, sentinel}
	hooks := backing[:1]

	m, err := ToParamMap(context.Background(), struct{ A, B int }{1, 2}, "", EncodeConfig{EncodeHooks: hooks}, nil)
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"A": 1, "B": 2}, m)
	assert.Equal(t, 2, calls)
	res, _, _ := backing[1](nil, nil, reflect.Value{})
	assert.Equal(t, "sentinel", res)
}
