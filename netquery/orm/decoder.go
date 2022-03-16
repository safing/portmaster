package orm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"time"

	"zombiezen.com/go/sqlite"
)

// Commonly used error messages when working with orm.
var (
	errStructExpected        = errors.New("encode: can only encode structs to maps")
	errStructPointerExpected = errors.New("decode: result must be pointer to a struct type or map[string]interface{}")
	errUnexpectedColumnType  = errors.New("decode: unexpected column type")
)

// constants used when transforming data to and from sqlite.
var (
	// sqliteTimeFromat defines the string representation that is
	// expected by SQLite DATETIME functions.
	// Note that SQLite itself does not include support for a DATETIME
	// column type. Instead, dates and times are stored either as INTEGER,
	// TEXT or REAL.
	// This package provides support for time.Time being stored as TEXT (using a
	// preconfigured timezone; UTC by default) or as INTEGER (the user can choose between
	// unixepoch and unixnano-epoch where the nano variant is not offically supported by
	// SQLITE).
	sqliteTimeFormat = "2006-01-02 15:04:05"
)

type (

	// Stmt describes the interface that must be implemented in order to
	// be decodable to a struct type using DecodeStmt. This interface is implemented
	// by *sqlite.Stmt.
	Stmt interface {
		ColumnCount() int
		ColumnName(int) string
		ColumnType(int) sqlite.ColumnType
		ColumnText(int) string
		ColumnBool(int) bool
		ColumnFloat(int) float64
		ColumnInt(int) int
		ColumnReader(int) *bytes.Reader
	}

	// DecodeFunc is called for each non-basic type during decoding.
	DecodeFunc func(colIdx int, stmt Stmt, fieldDef reflect.StructField, outval reflect.Value) (interface{}, error)

	DecodeConfig struct {
		DecodeHooks []DecodeFunc
	}
)

// DecodeStmt decodes the current result row loaded in Stmt into the struct or map type result.
// Decoding hooks configured in cfg are executed before trying to decode basic types and may
// be specified to provide support for special types.
// See DatetimeDecoder() for an example of a DecodeHook that handles graceful time.Time conversion.
//
func DecodeStmt(ctx context.Context, stmt Stmt, result interface{}, cfg DecodeConfig) error {
	// make sure we got something to decode into ...
	if result == nil {
		return fmt.Errorf("%w, got %T", errStructPointerExpected, result)
	}

	// fast path for decoding into a map
	if mp, ok := result.(*map[string]interface{}); ok {
		return decodeIntoMap(ctx, stmt, mp)
	}

	// make sure we got a pointer in result
	if reflect.TypeOf(result).Kind() != reflect.Ptr {
		return fmt.Errorf("%w, got %T", errStructPointerExpected, result)
	}

	// make sure it's a poiter to a struct type
	t := reflect.ValueOf(result).Elem().Type()
	if t.Kind() != reflect.Struct {
		return fmt.Errorf("%w, got %T", errStructPointerExpected, result)
	}

	// if result is a nil pointer make sure to allocate some space
	// for the resulting struct
	resultValue := reflect.ValueOf(result)
	if resultValue.IsNil() {
		resultValue.Set(
			reflect.New(t),
		)
	}

	// we need access to the struct directly and not to the
	// pointer.
	target := reflect.Indirect(resultValue)

	// create a lookup map from field name (or sqlite:"" tag)
	// to the field name
	lm := make(map[string]string)
	for i := 0; i < target.NumField(); i++ {
		fieldType := t.Field(i)

		// skip unexported fields
		if !fieldType.IsExported() {
			continue
		}

		lm[sqlColumnName(fieldType)] = fieldType.Name
	}

	// iterate over all columns and assign them to the correct
	// fields
	for i := 0; i < stmt.ColumnCount(); i++ {
		colName := stmt.ColumnName(i)
		fieldName, ok := lm[colName]
		if !ok {
			// there's no target field for this column
			// so we can skip it
			continue
		}
		fieldType, _ := t.FieldByName(fieldName)

		value := target.FieldByName(fieldName)

		colType := stmt.ColumnType(i)

		// if the column is reported as NULL we keep
		// the field as it is.
		// TODO(ppacher): should we set it to nil here?
		if colType == sqlite.TypeNull {
			continue
		}

		// if value is a nil pointer we need to allocate some memory
		// first
		if getKind(value) == reflect.Ptr && value.IsNil() {
			storage := reflect.New(fieldType.Type.Elem())

			value.Set(storage)

			// make sure value actually points the
			// dereferenced target storage
			value = storage.Elem()
		}

		// execute all decode hooks but make sure we use decodeBasic() as the
		// last one.
		columnValue, err := runDecodeHooks(
			i,
			stmt,
			fieldType,
			value,
			append(cfg.DecodeHooks, decodeBasic()),
		)
		if err != nil {
			return err
		}

		// if we don't have a converted value now we try to
		// decode basic types
		if columnValue == nil {
			return fmt.Errorf("cannot decode column %d (type=%s)", i, colType)
		}

		//log.Printf("valueTypeName: %s fieldName = %s value-orig = %s value = %s (%v) newValue = %s", value.Type().String(), fieldName, target.FieldByName(fieldName).Type(), value.Type(), value, columnValue)

		// convert it to the target type if conversion is possible
		newValue := reflect.ValueOf(columnValue)
		if newValue.Type().ConvertibleTo(value.Type()) {
			newValue = newValue.Convert(value.Type())
		}

		// assign the new value to the struct field.
		value.Set(newValue)
	}

	return nil
}

// DatetimeDecoder is capable of decoding sqlite INTEGER or TEXT storage classes into
// time.Time. For INTEGER storage classes, it supports 'unixnano' struct tag value to
// decide between Unix or UnixNano epoch timestamps.
//
// FIXME(ppacher): update comment about loc parameter and TEXT storage class parsing
//
func DatetimeDecoder(loc *time.Location) DecodeFunc {
	return func(colIdx int, stmt Stmt, fieldDef reflect.StructField, outval reflect.Value) (interface{}, error) {
		// we only care about "time.Time" here
		if outval.Type().String() != "time.Time" {
			return nil, nil
		}

		switch stmt.ColumnType(colIdx) {
		case sqlite.TypeInteger:
			// stored as unix-epoch, if unixnano is set in the struct field tag
			// we parse it with nano-second resolution
			// TODO(ppacher): actually split the tag value at "," and search
			// the slice for "unixnano"
			if strings.Contains(fieldDef.Tag.Get("sqlite"), ",unixnano") {
				return time.Unix(0, int64(stmt.ColumnInt(colIdx))), nil
			}

			return time.Unix(int64(stmt.ColumnInt(colIdx)), 0), nil

		case sqlite.TypeText:
			// stored ISO8601 but does not have any timezone information
			// assigned so we always treat it as loc here.
			t, err := time.ParseInLocation(sqliteTimeFormat, stmt.ColumnText(colIdx), loc)
			if err != nil {
				return nil, fmt.Errorf("failed to parse %q in %s: %w", stmt.ColumnText(colIdx), fieldDef.Name, err)
			}

			return t, nil

		case sqlite.TypeFloat:
			// stored as Julian day numbers
			return nil, fmt.Errorf("REAL storage type not support for time.Time")

		default:
			return nil, fmt.Errorf("unsupported storage type for time.Time: %s", outval.Type())
		}
	}
}

func decodeIntoMap(ctx context.Context, stmt Stmt, mp *map[string]interface{}) error {
	if *mp == nil {
		*mp = make(map[string]interface{})
	}

	for i := 0; i < stmt.ColumnCount(); i++ {
		var x interface{}
		val, err := decodeBasic()(i, stmt, reflect.StructField{}, reflect.ValueOf(&x).Elem())
		if err != nil {
			return fmt.Errorf("failed to decode column %s: %w", stmt.ColumnName(i), err)
		}

		(*mp)[stmt.ColumnName(i)] = val
	}

	return nil
}

func decodeBasic() DecodeFunc {
	return func(colIdx int, stmt Stmt, fieldDef reflect.StructField, outval reflect.Value) (interface{}, error) {
		valueKind := getKind(outval)
		colType := stmt.ColumnType(colIdx)
		colName := stmt.ColumnName(colIdx)

		errInvalidType := fmt.Errorf("%w %s for column %s with field type %s", errUnexpectedColumnType, colType.String(), colName, outval.Type())

		switch valueKind {
		case reflect.String:
			if colType != sqlite.TypeText {
				return nil, errInvalidType
			}
			return stmt.ColumnText(colIdx), nil

		case reflect.Bool:
			// sqlite does not have a BOOL type, it rather stores a 1/0 in a column
			// with INTEGER affinity.
			if colType != sqlite.TypeInteger {
				return nil, errInvalidType
			}
			return stmt.ColumnBool(colIdx), nil

		case reflect.Float64:
			if colType != sqlite.TypeFloat {
				return nil, errInvalidType
			}
			return stmt.ColumnFloat(colIdx), nil

		case reflect.Int, reflect.Uint: // getKind() normalizes all ints to reflect.Int/Uint because sqlite doesn't really care ...
			if colType != sqlite.TypeInteger {
				return nil, errInvalidType
			}

			return stmt.ColumnInt(colIdx), nil

		case reflect.Slice:
			if outval.Type().Elem().Kind() != reflect.Uint8 {
				return nil, fmt.Errorf("slices other than []byte for BLOB are not supported")
			}

			if colType != sqlite.TypeBlob {
				return nil, errInvalidType
			}

			columnValue, err := io.ReadAll(stmt.ColumnReader(colIdx))
			if err != nil {
				return nil, fmt.Errorf("failed to read blob for column %s: %w", fieldDef.Name, err)
			}

			return columnValue, nil

		case reflect.Interface:
			var (
				t reflect.Type
				x interface{}
			)
			switch colType {
			case sqlite.TypeBlob:
				t = reflect.TypeOf([]byte{})
				columnValue, err := io.ReadAll(stmt.ColumnReader(colIdx))
				if err != nil {
					return nil, fmt.Errorf("failed to read blob for column %s: %w", fieldDef.Name, err)
				}
				x = columnValue

			case sqlite.TypeFloat:
				t = reflect.TypeOf(float64(0))
				x = stmt.ColumnFloat(colIdx)

			case sqlite.TypeInteger:
				t = reflect.TypeOf(int(0))
				x = stmt.ColumnInt(colIdx)

			case sqlite.TypeText:
				t = reflect.TypeOf(string(""))
				x = stmt.ColumnText(colIdx)

			case sqlite.TypeNull:
				t = nil
				x = nil

			default:
				return nil, fmt.Errorf("unsupported column type %s", colType)
			}

			if t == nil {
				return nil, nil
			}

			target := reflect.New(t).Elem()
			target.Set(reflect.ValueOf(x))

			return target.Interface(), nil

		default:
			return nil, fmt.Errorf("cannot decode into %s", valueKind)
		}
	}
}

func sqlColumnName(fieldType reflect.StructField) string {
	tagValue, hasTag := fieldType.Tag.Lookup("sqlite")
	if !hasTag {
		return fieldType.Name
	}

	parts := strings.Split(tagValue, ",")
	if parts[0] != "" {
		return parts[0]
	}

	return fieldType.Name
}

// runDecodeHooks tries to decode the column value of stmt at index colIdx into outval by running all decode hooks.
// The first hook that returns a non-nil interface wins, other hooks will not be executed. If an error is
// returned by a decode hook runDecodeHooks stops the error is returned to the caller.
func runDecodeHooks(colIdx int, stmt Stmt, fieldDef reflect.StructField, outval reflect.Value, hooks []DecodeFunc) (interface{}, error) {
	for _, fn := range hooks {
		res, err := fn(colIdx, stmt, fieldDef, outval)
		if err != nil {
			return res, err
		}

		if res != nil {
			return res, nil
		}
	}

	return nil, nil
}

// getKind returns the kind of value but normalized Int, Uint and Float varaints
// to their base type.
func getKind(val reflect.Value) reflect.Kind {
	kind := val.Kind()
	return normalizeKind(kind)
}

func normalizeKind(kind reflect.Kind) reflect.Kind {
	switch {
	case kind >= reflect.Int && kind <= reflect.Int64:
		return reflect.Int
	case kind >= reflect.Uint && kind <= reflect.Uint64:
		return reflect.Uint
	case kind >= reflect.Float32 && kind <= reflect.Float64:
		return reflect.Float64
	default:
		return kind
	}
}

var DefaultDecodeConfig = DecodeConfig{
	DecodeHooks: []DecodeFunc{
		DatetimeDecoder(time.UTC),
	},
}
