package orm

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"zombiezen.com/go/sqlite"
)

type testStmt struct {
	columns []string
	values  []interface{}
	types   []sqlite.ColumnType
}

func (ts testStmt) ColumnCount() int                   { return len(ts.columns) }
func (ts testStmt) ColumnName(i int) string            { return ts.columns[i] }
func (ts testStmt) ColumnBool(i int) bool              { return ts.values[i].(bool) }
func (ts testStmt) ColumnText(i int) string            { return ts.values[i].(string) }
func (ts testStmt) ColumnFloat(i int) float64          { return ts.values[i].(float64) }
func (ts testStmt) ColumnInt(i int) int                { return ts.values[i].(int) }
func (ts testStmt) ColumnReader(i int) *bytes.Reader   { return bytes.NewReader(ts.values[i].([]byte)) }
func (ts testStmt) ColumnType(i int) sqlite.ColumnType { return ts.types[i] }

// compile time check
var _ Stmt = new(testStmt)

type exampleFieldTypes struct {
	S string
	I int
	F float64
	B bool
}

type examplePointerTypes struct {
	S *string
	I *int
	F *float64
	B *bool
}

type exampleStructTags struct {
	S string `sqlite:"col_string"`
	I int    `sqlite:"col_int"`
}

type exampleIntConv struct {
	I8  int8
	I16 int16
	I32 int32
	I64 int64
	I   int
}

type exampleBlobTypes struct {
	B []byte
}

type exampleJSONRawTypes struct {
	B json.RawMessage
}

type exampleTimeTypes struct {
	T  time.Time
	TP *time.Time
}

type exampleInterface struct {
	I  interface{}
	IP *interface{}
}

func (ett *exampleTimeTypes) Equal(other interface{}) bool {
	oett, ok := other.(*exampleTimeTypes)
	if !ok {
		return false
	}
	return ett.T.Equal(oett.T) && (ett.TP != nil && oett.TP != nil && ett.TP.Equal(*oett.TP)) || (ett.TP == nil && oett.TP == nil)
}

type exampleTimeNano struct {
	T time.Time `sqlite:",unixnano"`
}

func (etn *exampleTimeNano) Equal(other interface{}) bool {
	oetn, ok := other.(*exampleTimeNano)
	if !ok {
		return false
	}
	return etn.T.Equal(oetn.T)
}

func Test_Decoder(t *testing.T) {
	ctx := context.TODO()

	refTime := time.Date(2022, time.February, 15, 9, 51, 00, 00, time.UTC)

	cases := []struct {
		Desc     string
		Stmt     testStmt
		Result   interface{}
		Expected interface{}
	}{
		{
			"Decoding into nil is not allowed",
			testStmt{
				columns: nil,
				values:  nil,
				types:   nil,
			},
			nil,
			nil,
		},
		{
			"Decoding into basic types",
			testStmt{
				columns: []string{"S", "I", "F", "B"},
				types: []sqlite.ColumnType{
					sqlite.TypeText,
					sqlite.TypeInteger,
					sqlite.TypeFloat,
					sqlite.TypeInteger,
				},
				values: []interface{}{
					"string value",
					1,
					1.2,
					true,
				},
			},
			&exampleFieldTypes{},
			&exampleFieldTypes{
				S: "string value",
				I: 1,
				F: 1.2,
				B: true,
			},
		},
		{
			"Decoding into basic types with different order",
			testStmt{
				columns: []string{"I", "S", "B", "F"},
				types: []sqlite.ColumnType{
					sqlite.TypeInteger,
					sqlite.TypeText,
					sqlite.TypeInteger,
					sqlite.TypeFloat,
				},
				values: []interface{}{
					1,
					"string value",
					true,
					1.2,
				},
			},
			&exampleFieldTypes{},
			&exampleFieldTypes{
				S: "string value",
				I: 1,
				F: 1.2,
				B: true,
			},
		},
		{
			"Decoding into basic types with missing values",
			testStmt{
				columns: []string{"F", "B"},
				types: []sqlite.ColumnType{
					sqlite.TypeFloat,
					sqlite.TypeInteger,
				},
				values: []interface{}{
					1.2,
					true,
				},
			},
			&exampleFieldTypes{},
			&exampleFieldTypes{
				F: 1.2,
				B: true,
			},
		},
		{
			"Decoding into pointer types",
			testStmt{
				columns: []string{"S", "I", "F", "B"},
				types: []sqlite.ColumnType{
					sqlite.TypeText,
					sqlite.TypeInteger,
					sqlite.TypeFloat,
					sqlite.TypeInteger,
				},
				values: []interface{}{
					"string value",
					1,
					1.2,
					true,
				},
			},
			&examplePointerTypes{},
			func() interface{} {
				s := "string value"
				i := 1
				f := 1.2
				b := true

				return &examplePointerTypes{
					S: &s,
					I: &i,
					F: &f,
					B: &b,
				}
			},
		},
		{
			"Decoding into pointer types with missing values",
			testStmt{
				columns: []string{"S", "B"},
				types: []sqlite.ColumnType{
					sqlite.TypeText,
					sqlite.TypeInteger,
					sqlite.TypeFloat,
					sqlite.TypeInteger,
				},
				values: []interface{}{
					"string value",
					true,
				},
			},
			&examplePointerTypes{},
			func() interface{} {
				s := "string value"
				b := true

				return &examplePointerTypes{
					S: &s,
					B: &b,
				}
			},
		},
		{
			"Decoding into fields with struct tags",
			testStmt{
				columns: []string{"col_string", "col_int"},
				types: []sqlite.ColumnType{
					sqlite.TypeText,
					sqlite.TypeInteger,
				},
				values: []interface{}{
					"string value",
					1,
				},
			},
			&exampleStructTags{},
			&exampleStructTags{
				S: "string value",
				I: 1,
			},
		},
		{
			"Decoding into correct int type",
			testStmt{
				columns: []string{"I8", "I16", "I32", "I64", "I"},
				types: []sqlite.ColumnType{
					sqlite.TypeInteger,
					sqlite.TypeInteger,
					sqlite.TypeInteger,
					sqlite.TypeInteger,
					sqlite.TypeInteger,
				},
				values: []interface{}{
					1,
					1,
					1,
					1,
					1,
				},
			},
			&exampleIntConv{},
			&exampleIntConv{
				1, 1, 1, 1, 1,
			},
		},
		{
			"Handling NULL values for basic types",
			testStmt{
				columns: []string{"S", "I", "F"},
				types: []sqlite.ColumnType{
					sqlite.TypeNull,
					sqlite.TypeNull,
					sqlite.TypeFloat,
				},
				values: []interface{}{
					// we use nil here but actually that does not matter
					nil,
					nil,
					1.0,
				},
			},
			&exampleFieldTypes{},
			&exampleFieldTypes{
				F: 1.0,
			},
		},
		{
			"Handling NULL values for pointer types",
			testStmt{
				columns: []string{"S", "I", "F"},
				types: []sqlite.ColumnType{
					sqlite.TypeNull,
					sqlite.TypeNull,
					sqlite.TypeFloat,
				},
				values: []interface{}{
					// we use nil here but actually that does not matter
					nil,
					nil,
					1.0,
				},
			},
			&examplePointerTypes{},
			func() interface{} {
				f := 1.0

				return &examplePointerTypes{F: &f}
			},
		},
		{
			"Handling blob types",
			testStmt{
				columns: []string{"B"},
				types: []sqlite.ColumnType{
					sqlite.TypeBlob,
				},
				values: []interface{}{
					([]byte)("hello world"),
				},
			},
			&exampleBlobTypes{},
			&exampleBlobTypes{
				B: ([]byte)("hello world"),
			},
		},
		{
			"Handling blob types as json.RawMessage",
			testStmt{
				columns: []string{"B"},
				types: []sqlite.ColumnType{
					sqlite.TypeBlob,
				},
				values: []interface{}{
					([]byte)("hello world"),
				},
			},
			&exampleJSONRawTypes{},
			&exampleJSONRawTypes{
				B: (json.RawMessage)("hello world"),
			},
		},
		{
			"Handling time.Time and pointers to it",
			testStmt{
				columns: []string{"T", "TP"},
				types: []sqlite.ColumnType{
					sqlite.TypeInteger,
					sqlite.TypeInteger,
				},
				values: []interface{}{
					int(refTime.Unix()),
					int(refTime.Unix()),
				},
			},
			&exampleTimeTypes{},
			&exampleTimeTypes{
				T:  refTime,
				TP: &refTime,
			},
		},
		{
			"Handling time.Time in nano-second resolution (struct tags)",
			testStmt{
				columns: []string{"T", "TP"},
				types: []sqlite.ColumnType{
					sqlite.TypeInteger,
					sqlite.TypeInteger,
				},
				values: []interface{}{
					int(refTime.UnixNano()),
					int(refTime.UnixNano()),
				},
			},
			&exampleTimeNano{},
			&exampleTimeNano{
				T: refTime,
			},
		},
		{
			"Decoding into interface",
			testStmt{
				columns: []string{"I", "IP"},
				types: []sqlite.ColumnType{
					sqlite.TypeText,
					sqlite.TypeText,
				},
				values: []interface{}{
					"value1",
					"value2",
				},
			},
			&exampleInterface{},
			func() interface{} {
				var x interface{}
				x = "value2"

				return &exampleInterface{
					I:  "value1",
					IP: &x,
				}
			},
		},
		{
			"Decoding into map[string]interface{}",
			testStmt{
				columns: []string{"I", "F", "S", "B"},
				types: []sqlite.ColumnType{
					sqlite.TypeInteger,
					sqlite.TypeFloat,
					sqlite.TypeText,
					sqlite.TypeBlob,
				},
				values: []interface{}{
					1,
					1.1,
					"string value",
					[]byte("blob value"),
				},
			},
			new(map[string]interface{}),
			&map[string]interface{}{
				"I": 1,
				"F": 1.1,
				"S": "string value",
				"B": []byte("blob value"),
			},
		},
	}

	for idx := range cases {
		c := cases[idx]
		t.Run(c.Desc, func(t *testing.T) {
			t.Parallel()

			err := DecodeStmt(ctx, c.Stmt, c.Result, DefaultDecodeConfig)
			if fn, ok := c.Expected.(func() interface{}); ok {
				c.Expected = fn()
			}

			if c.Expected == nil {
				assert.Error(t, err, c.Desc)
			} else {
				assert.NoError(t, err, c.Desc)

				if equaler, ok := c.Expected.(interface{ Equal(x interface{}) bool }); ok {
					assert.True(t, equaler.Equal(c.Result))
				} else {
					assert.Equal(t, c.Expected, c.Result)
				}
			}
		})
	}
}
