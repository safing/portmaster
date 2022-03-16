package orm

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func Test_EncodeAsMap(t *testing.T) {
	ctx := context.TODO()
	refTime := time.Date(2022, time.February, 15, 9, 51, 00, 00, time.UTC)

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
				"TinString": refTime.Format(sqliteTimeFormat),
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
				"TinString": refTime.Format(sqliteTimeFormat),
				"Tnil1":     nil,
				"Tnil2":     nil,
			},
		},
	}

	for idx := range cases {
		c := cases[idx]
		t.Run(c.Desc, func(t *testing.T) {
			// t.Parallel()

			res, err := EncodeAsMap(ctx, c.Input, "", DefaultEncodeConfig)
			assert.NoError(t, err)
			assert.Equal(t, c.Expected, res)
		})
	}
}
