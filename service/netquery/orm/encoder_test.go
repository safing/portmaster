package orm

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"zombiezen.com/go/sqlite"
)

func TestEncodeAsMap(t *testing.T) { //nolint:tparallel
	t.Parallel()

	ctx := context.TODO()
	refTime := time.Date(2022, time.February, 15, 9, 51, 0, 0, time.UTC)

	cases := []struct {
		Desc     string
		Input    interface{}
		Expected map[string]interface{}
	}{
		{
			"Encode basic types",
			struct {
				I int
				F float64
				S string
				B []byte
			}{
				I: 1,
				F: 1.2,
				S: "string",
				B: ([]byte)("bytes"),
			},
			map[string]interface{}{
				"I": 1,
				"F": 1.2,
				"S": "string",
				"B": ([]byte)("bytes"),
			},
		},
		{
			"Encode using struct tags",
			struct {
				I int    `sqlite:"col_int"`
				S string `sqlite:"col_string"`
			}{
				I: 1,
				S: "string value",
			},
			map[string]interface{}{
				"col_int":    1,
				"col_string": "string value",
			},
		},
		{
			"Ignore Private fields",
			struct {
				I int
				s string
			}{
				I: 1,
				s: "string value",
			},
			map[string]interface{}{
				"I": 1,
			},
		},
		{
			"Handle Pointers",
			struct {
				I *int
				S *string
			}{
				I: new(int),
			},
			map[string]interface{}{
				"I": 0,
				"S": nil,
			},
		},
		{
			"Handle time.Time types",
			struct {
				TinInt    time.Time `sqlite:",integer,unixnano"`
				TinString time.Time `sqlite:",text"`
			}{
				TinInt:    refTime,
				TinString: refTime,
			},
			map[string]interface{}{
				"TinInt":    refTime.UnixNano(),
				"TinString": refTime.Format(SqliteTimeFormat),
			},
		},
		{
			"Handle time.Time pointer types",
			struct {
				TinInt    *time.Time `sqlite:",integer,unixnano"`
				TinString *time.Time `sqlite:",text"`
				Tnil1     *time.Time `sqlite:",text"`
				Tnil2     *time.Time `sqlite:",text"`
			}{
				TinInt:    &refTime,
				TinString: &refTime,
				Tnil1:     nil,
				Tnil2:     (*time.Time)(nil),
			},
			map[string]interface{}{
				"TinInt":    refTime.UnixNano(),
				"TinString": refTime.Format(SqliteTimeFormat),
				"Tnil1":     nil,
				"Tnil2":     nil,
			},
		},
	}

	for idx := range cases { //nolint:paralleltest
		c := cases[idx]
		t.Run(c.Desc, func(t *testing.T) {
			res, err := ToParamMap(ctx, c.Input, "", DefaultEncodeConfig, nil)
			require.NoError(t, err)
			assert.Equal(t, c.Expected, res)
		})
	}
}

func TestEncodeValue(t *testing.T) { //nolint:tparallel
	t.Parallel()

	ctx := context.TODO()
	refTime := time.Date(2022, time.February, 15, 9, 51, 0, 0, time.UTC)

	cases := []struct {
		Desc   string
		Column ColumnDef
		Input  interface{}
		Output interface{}
	}{
		{
			"Special value time.Time as text",
			ColumnDef{
				IsTime: true,
				Type:   sqlite.TypeText,
			},
			refTime,
			refTime.Format(SqliteTimeFormat),
		},
		{
			"Special value time.Time as unix-epoch",
			ColumnDef{
				IsTime: true,
				Type:   sqlite.TypeInteger,
			},
			refTime,
			refTime.Unix(),
		},
		{
			"Special value time.Time as unixnano-epoch",
			ColumnDef{
				IsTime:   true,
				Type:     sqlite.TypeInteger,
				UnixNano: true,
			},
			refTime,
			refTime.UnixNano(),
		},
		{
			"Special value zero time",
			ColumnDef{
				IsTime: true,
				Type:   sqlite.TypeText,
			},
			time.Time{},
			nil,
		},
		{
			"Special value zero time pointer",
			ColumnDef{
				IsTime: true,
				Type:   sqlite.TypeText,
			},
			new(time.Time),
			nil,
		},
		{
			"Special value *time.Time as text",
			ColumnDef{
				IsTime: true,
				Type:   sqlite.TypeText,
			},
			&refTime,
			refTime.Format(SqliteTimeFormat),
		},
		{
			"Special value untyped nil",
			ColumnDef{
				Nullable: true,
				IsTime:   true,
				Type:     sqlite.TypeText,
			},
			nil,
			nil,
		},
		{
			"Special value typed nil",
			ColumnDef{
				IsTime: true,
				Type:   sqlite.TypeText,
			},
			(*time.Time)(nil),
			nil,
		},
		{
			"Time formated as string",
			ColumnDef{
				IsTime: true,
				Type:   sqlite.TypeText,
			},
			refTime.In(time.Local).Format(time.RFC3339),
			refTime.Format(SqliteTimeFormat),
		},
		{
			"Nullable integer",
			ColumnDef{
				Type:     sqlite.TypeInteger,
				Nullable: true,
			},
			nil,
			nil,
		},
		{
			"Not-Null integer",
			ColumnDef{
				Name: "test",
				Type: sqlite.TypeInteger,
			},
			nil,
			0,
		},
		{
			"Not-Null string",
			ColumnDef{
				Type: sqlite.TypeText,
			},
			nil,
			"",
		},
	}

	for idx := range cases { //nolint:paralleltest
		c := cases[idx]
		t.Run(c.Desc, func(t *testing.T) {
			res, err := EncodeValue(ctx, &c.Column, c.Input, DefaultEncodeConfig)
			require.NoError(t, err)
			assert.Equal(t, c.Output, res)
		})
	}
}
