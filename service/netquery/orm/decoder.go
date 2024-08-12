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
	// unixepoch and unixnano-epoch where the nano variant is not officially supported by
	// SQLITE).
	SqliteTimeFormat = "2006-01-02 15:04:05"
)

type (

	// Stmt describes the interface that must be implemented in order to
	// be decodable to a struct type using DecodeStmt. This interface is implemented
	// by *sqlite.Stmt.
	Stmt interface {
		ColumnCount() int
		ColumnName(col int) string
		ColumnType(col int) sqlite.ColumnType
		ColumnText(col int) string
		ColumnBool(col int) bool
		ColumnFloat(col int) float64
		ColumnInt(col int) int
		ColumnReader(col int) *bytes.Reader
	}

	// DecodeFunc is called for each non-basic type during decoding.
	DecodeFunc func(colIdx int, colDef *ColumnDef, stmt Stmt, fieldDef reflect.StructField, outval reflect.Value) (interface{}, bool, error)

	// DecodeConfig holds decoding functions.
	DecodeConfig struct {
		DecodeHooks []DecodeFunc
	}
)

// DecodeStmt decodes the current result row loaded in Stmt into the struct or map type result.
// Decoding hooks configured in cfg are executed before trying to decode basic types and may
// be specified to provide support for special types.
// See DatetimeDecoder() for an example of a DecodeHook that handles graceful time.Time conversion.
func DecodeStmt(ctx context.Context, schema *TableSchema, stmt Stmt, result interface{}, cfg DecodeConfig) error {
	// make sure we got something to decode into ...
	if result == nil {
		return fmt.Errorf("%w, got %T", errStructPointerExpected, result)
	}

	// fast path for decoding into a map
	if mp, ok := result.(*map[string]interface{}); ok {
		return decodeIntoMap(ctx, schema, stmt, mp, cfg)
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
	for i := range target.NumField() {
		fieldType := t.Field(i)

		// skip unexported fields
		if !fieldType.IsExported() {
			continue
		}

		lm[sqlColumnName(fieldType)] = fieldType.Name
	}

	// iterate over all columns and assign them to the correct
	// fields
	for i := range stmt.ColumnCount() {
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

		colDef := schema.GetColumnDef(colName)

		// execute all decode hooks but make sure we use decodeBasic() as the
		// last one.
		columnValue, err := runDecodeHooks(
			i,
			colDef,
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

		// Debugging:
		// log.Printf("valueTypeName: %s fieldName = %s value-orig = %s value = %s (%v) newValue = %s", value.Type().String(), fieldName, target.FieldByName(fieldName).Type(), value.Type(), value, columnValue)

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
// TODO(ppacher): update comment about loc parameter and TEXT storage class parsing.
func DatetimeDecoder(loc *time.Location) DecodeFunc {
	return func(colIdx int, colDef *ColumnDef, stmt Stmt, fieldDef reflect.StructField, outval reflect.Value) (interface{}, bool, error) {
		// if we have the column definition available we
		// use the target go type from there.
		outType := outval.Type()

		if colDef != nil {
			outType = colDef.GoType
		}

		// we only care about "time.Time" here
		if outType.String() != "time.Time" || (colDef != nil && !colDef.IsTime) {
			// log.Printf("not decoding %s %v", outType, colDef)
			return nil, false, nil
		}

		switch stmt.ColumnType(colIdx) { //nolint:exhaustive // Only selecting specific types.
		case sqlite.TypeInteger:
			// stored as unix-epoch, if unixnano is set in the struct field tag
			// we parse it with nano-second resolution
			// TODO(ppacher): actually split the tag value at "," and search
			// the slice for "unixnano"
			if strings.Contains(fieldDef.Tag.Get("sqlite"), ",unixnano") {
				return time.Unix(0, int64(stmt.ColumnInt(colIdx))), true, nil
			}

			return time.Unix(int64(stmt.ColumnInt(colIdx)), 0), true, nil

		case sqlite.TypeText:
			// stored ISO8601 but does not have any timezone information
			// assigned so we always treat it as loc here.
			t, err := time.ParseInLocation(SqliteTimeFormat, stmt.ColumnText(colIdx), loc)
			if err != nil {
				return nil, false, fmt.Errorf("failed to parse %q in %s: %w", stmt.ColumnText(colIdx), fieldDef.Name, err)
			}

			return t, true, nil

		case sqlite.TypeFloat:
			// stored as Julian day numbers
			return nil, false, errors.New("REAL storage type not support for time.Time")

		case sqlite.TypeNull:
			return nil, true, nil

		default:
			return nil, false, fmt.Errorf("unsupported storage type for time.Time: %s", stmt.ColumnType(colIdx))
		}
	}
}

func decodeIntoMap(_ context.Context, schema *TableSchema, stmt Stmt, mp *map[string]interface{}, cfg DecodeConfig) error {
	if *mp == nil {
		*mp = make(map[string]interface{})
	}

	for i := range stmt.ColumnCount() {
		var x interface{}

		colDef := schema.GetColumnDef(stmt.ColumnName(i))

		outVal := reflect.ValueOf(&x).Elem()
		fieldType := reflect.StructField{}
		if colDef != nil {
			outVal = reflect.New(colDef.GoType).Elem()
			fieldType = reflect.StructField{
				Type: colDef.GoType,
			}
		}

		val, err := runDecodeHooks(
			i,
			colDef,
			stmt,
			fieldType,
			outVal,
			append(cfg.DecodeHooks, decodeBasic()),
		)
		if err != nil {
			return fmt.Errorf("failed to decode column %s: %w", stmt.ColumnName(i), err)
		}

		(*mp)[stmt.ColumnName(i)] = val
	}

	return nil
}

func decodeBasic() DecodeFunc {
	return func(colIdx int, colDef *ColumnDef, stmt Stmt, fieldDef reflect.StructField, outval reflect.Value) (result interface{}, handled bool, err error) {
		valueKind := getKind(outval)
		colType := stmt.ColumnType(colIdx)
		colName := stmt.ColumnName(colIdx)

		errInvalidType := fmt.Errorf("%w %s for column %s with field type %s", errUnexpectedColumnType, colType.String(), colName, outval.Type())

		// if we have the column definition available we
		// use the target go type from there.
		if colDef != nil {
			valueKind = NormalizeKind(colDef.GoType.Kind())

			// if we have a column definition we try to convert the value to
			// the actual Go-type that was used in the model.
			// this is useful, for example, to ensure a []byte{} is always decoded into json.RawMessage
			// or that type aliases like (type myInt int) are decoded into myInt instead of int
			defer func() {
				if handled {
					t := reflect.New(colDef.GoType).Elem()

					if result == nil || reflect.ValueOf(result).IsZero() {
						return
					}

					if reflect.ValueOf(result).Type().ConvertibleTo(colDef.GoType) {
						result = reflect.ValueOf(result).Convert(colDef.GoType).Interface()
					}
					t.Set(reflect.ValueOf(result))

					result = t.Interface()
				}
			}()
		}

		// log.Printf("decoding %s into kind %s", colName, valueKind)

		if colType == sqlite.TypeNull {
			if colDef != nil && colDef.Nullable {
				return nil, true, nil
			}

			if colDef != nil && !colDef.Nullable {
				return reflect.New(colDef.GoType).Elem().Interface(), true, nil
			}

			if outval.Kind() == reflect.Ptr {
				return nil, true, nil
			}
		}

		switch valueKind { //nolint:exhaustive
		case reflect.String:
			if colType != sqlite.TypeText {
				return nil, false, errInvalidType
			}
			return stmt.ColumnText(colIdx), true, nil

		case reflect.Bool:
			// sqlite does not have a BOOL type, it rather stores a 1/0 in a column
			// with INTEGER affinity.
			if colType != sqlite.TypeInteger {
				return nil, false, errInvalidType
			}
			return stmt.ColumnBool(colIdx), true, nil

		case reflect.Float64:
			if colType != sqlite.TypeFloat {
				return nil, false, errInvalidType
			}
			return stmt.ColumnFloat(colIdx), true, nil

		case reflect.Int, reflect.Uint: // getKind() normalizes all ints to reflect.Int/Uint because sqlite doesn't really care ...
			if colType != sqlite.TypeInteger {
				return nil, false, errInvalidType
			}

			return stmt.ColumnInt(colIdx), true, nil

		case reflect.Slice:
			if outval.Type().Elem().Kind() != reflect.Uint8 {
				return nil, false, errors.New("slices other than []byte for BLOB are not supported")
			}

			if colType != sqlite.TypeBlob {
				return nil, false, errInvalidType
			}

			columnValue, err := io.ReadAll(stmt.ColumnReader(colIdx))
			if err != nil {
				return nil, false, fmt.Errorf("failed to read blob for column %s: %w", fieldDef.Name, err)
			}

			return columnValue, true, nil

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
					return nil, false, fmt.Errorf("failed to read blob for column %s: %w", fieldDef.Name, err)
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
				return nil, false, fmt.Errorf("unsupported column type %s", colType)
			}

			if t == nil {
				return nil, true, nil
			}

			target := reflect.New(t).Elem()
			target.Set(reflect.ValueOf(x))

			return target.Interface(), true, nil

		default:
			return nil, false, fmt.Errorf("cannot decode into %s", valueKind)
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
func runDecodeHooks(colIdx int, colDef *ColumnDef, stmt Stmt, fieldDef reflect.StructField, outval reflect.Value, hooks []DecodeFunc) (interface{}, error) {
	for _, fn := range hooks {
		res, end, err := fn(colIdx, colDef, stmt, fieldDef, outval)
		if err != nil {
			return res, err
		}

		if end {
			return res, nil
		}
	}

	return nil, nil
}

// getKind returns the kind of value but normalized Int, Uint and Float variants.
// to their base type.
func getKind(val reflect.Value) reflect.Kind {
	kind := val.Kind()
	return NormalizeKind(kind)
}

// NormalizeKind returns a normalized kind of the given kind.
func NormalizeKind(kind reflect.Kind) reflect.Kind {
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

// DefaultDecodeConfig holds the default decoding configuration.
var DefaultDecodeConfig = DecodeConfig{
	DecodeHooks: []DecodeFunc{
		DatetimeDecoder(time.UTC),
	},
}
