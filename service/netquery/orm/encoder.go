package orm

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"time"

	"golang.org/x/exp/slices"
	"zombiezen.com/go/sqlite"
)

type (
	// EncodeFunc is called for each non-basic type during encoding.
	// The column definition is shared between calls and must not be modified.
	EncodeFunc func(col *ColumnDef, valType reflect.Type, val reflect.Value) (interface{}, bool, error)

	// EncodeConfig holds encoding functions.
	EncodeConfig struct {
		EncodeHooks []EncodeFunc
	}
)

// ToParamMap returns a map that contains the sqlite compatible value of each struct field of
// r using the sqlite column name as a map key. It either uses the name of the
// exported struct field or the value of the "sqlite" tag.
func ToParamMap(ctx context.Context, r interface{}, keyPrefix string, cfg EncodeConfig, skipFields []string) (map[string]interface{}, error) {
	// make sure we work on a struct type
	val := reflect.Indirect(reflect.ValueOf(r))
	if val.Kind() != reflect.Struct {
		return nil, fmt.Errorf("%w, got %T", errStructExpected, r)
	}

	fields := getEncoderFields(val.Type())
	res := make(map[string]interface{}, val.NumField())

	// Build the hook list once instead of for every field. Copy it, so that
	// the backing array of cfg.EncodeHooks is never written to.
	hooks := make([]EncodeFunc, 0, len(cfg.EncodeHooks)+1)
	hooks = append(hooks, cfg.EncodeHooks...)
	hooks = append(hooks, encodeBasic())

	for i := range fields {
		ef := &fields[i]
		fieldType := &ef.field
		field := val.Field(ef.index)

		if ef.colDefErr != nil {
			return nil, fmt.Errorf("failed to get column definition for %s: %w", fieldType.Name, ef.colDefErr)
		}
		colDef := ef.colDef

		if slices.Contains(skipFields, colDef.Name) {
			continue
		}

		x, found, err := runEncodeHooks(
			colDef,
			fieldType.Type,
			field,
			hooks,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to run encode hooks: %w", err)
		}

		if !found {
			if reflect.Indirect(field).IsValid() {
				x = reflect.Indirect(field).Interface()
			}
		}

		res[keyPrefix+ef.columnName] = x
	}

	return res, nil
}

// encoderField holds the per-type metadata of an exported struct field that
// ToParamMap needs to encode it.
type encoderField struct {
	index      int
	field      reflect.StructField
	colDef     *ColumnDef
	colDefErr  error
	columnName string
}

// encoderFieldsCache caches the encoder fields of struct types.
// It maps reflect.Type to []encoderField.
var encoderFieldsCache sync.Map

// getEncoderFields returns the metadata of all exported fields of the given
// struct type. Parsing the struct tags is only done once per type, as it
// otherwise dominates the cost of encoding a row.
func getEncoderFields(t reflect.Type) []encoderField {
	if cached, ok := encoderFieldsCache.Load(t); ok {
		return cached.([]encoderField) //nolint:forcetypeassert // Only this function stores values.
	}

	fields := make([]encoderField, 0, t.NumField())
	for i := range t.NumField() {
		fieldType := t.Field(i)

		// skip unexported fields
		if !fieldType.IsExported() {
			continue
		}

		colDef, err := getColumnDef(fieldType)
		fields = append(fields, encoderField{
			index:      i,
			field:      fieldType,
			colDef:     colDef,
			colDefErr:  err,
			columnName: sqlColumnName(fieldType),
		})
	}

	encoderFieldsCache.Store(t, fields)
	return fields
}

// EncodeValue encodes the given value.
func EncodeValue(ctx context.Context, colDef *ColumnDef, val interface{}, cfg EncodeConfig) (interface{}, error) {
	fieldValue := reflect.ValueOf(val)
	fieldType := reflect.TypeOf(val)

	x, found, err := runEncodeHooks(
		colDef,
		fieldType,
		fieldValue,
		append(
			cfg.EncodeHooks,
			encodeBasic(),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to run encode hooks: %w", err)
	}

	if !found {
		if reflect.Indirect(fieldValue).IsValid() {
			x = reflect.Indirect(fieldValue).Interface()
		}
	}

	return x, nil
}

func encodeBasic() EncodeFunc {
	return func(col *ColumnDef, valType reflect.Type, val reflect.Value) (interface{}, bool, error) {
		kind := valType.Kind()
		if kind == reflect.Ptr {
			valType = valType.Elem()
			kind = valType.Kind()

			if val.IsNil() {
				if !col.Nullable {
					// we need to set the zero value here since the column
					// is not marked as nullable
					return reflect.New(valType).Elem().Interface(), true, nil
				}

				return nil, true, nil
			}

			val = val.Elem()
		}

		switch NormalizeKind(kind) { //nolint:exhaustive
		case reflect.String,
			reflect.Float64,
			reflect.Bool,
			reflect.Int,
			reflect.Uint:
			// sqlite package handles conversion of those types
			// already
			return val.Interface(), true, nil

		case reflect.Slice:
			if valType.Elem().Kind() == reflect.Uint8 {
				// this is []byte
				return val.Interface(), true, nil
			}
			fallthrough

		default:
			return nil, false, fmt.Errorf("cannot convert value of kind %s for use in SQLite", kind)
		}
	}
}

// DatetimeEncoder returns a new datetime encoder for the given time zone.
func DatetimeEncoder(loc *time.Location) EncodeFunc {
	return func(colDef *ColumnDef, valType reflect.Type, val reflect.Value) (interface{}, bool, error) {
		// if fieldType holds a pointer we need to dereference the value
		ft := valType.String()
		if valType.Kind() == reflect.Ptr {
			ft = valType.Elem().String()
			val = reflect.Indirect(val)
		}

		normalizedKind := NormalizeKind(valType.Kind())

		// we only care about "time.Time" here
		var t time.Time
		switch {
		case ft == "time.Time":
			// handle the zero time as a NULL.
			if !val.IsValid() || val.IsZero() {
				return nil, true, nil
			}

			var ok bool
			valInterface := val.Interface()
			t, ok = valInterface.(time.Time)
			if !ok {
				return nil, false, errors.New("cannot convert reflect value to time.Time")
			}

		case valType.Kind() == reflect.String && colDef.IsTime:
			var err error
			t, err = time.Parse(time.RFC3339, val.String())
			if err != nil {
				return nil, false, fmt.Errorf("failed to parse time as RFC3339: %w", err)
			}

		case (normalizedKind == reflect.Int || normalizedKind == reflect.Uint || normalizedKind == reflect.Float64) && colDef.IsTime:
			seconds := int64(0)
			switch normalizedKind { //nolint:exhaustive // Previous switch case assures these types.
			case reflect.Int:
				seconds = val.Int()
			case reflect.Uint:
				seconds = int64(val.Uint())
			case reflect.Float64:
				seconds = int64(val.Float())
			}

			t = time.Unix(seconds, 0)

		default:
			// we don't care ...
			return nil, false, nil
		}

		switch colDef.Type { //nolint:exhaustive
		case sqlite.TypeInteger:
			if colDef.UnixNano {
				return t.UnixNano(), true, nil
			}
			return t.Unix(), true, nil

		case sqlite.TypeText:
			str := t.In(loc).Format(SqliteTimeFormat)

			return str, true, nil
		}

		return nil, false, fmt.Errorf("cannot store time.Time in %s", colDef.Type)
	}
}

func runEncodeHooks(colDef *ColumnDef, valType reflect.Type, val reflect.Value, hooks []EncodeFunc) (interface{}, bool, error) {
	if valType == nil {
		if !colDef.Nullable {
			switch colDef.Type { //nolint:exhaustive
			case sqlite.TypeBlob:
				return []byte{}, true, nil
			case sqlite.TypeFloat:
				return 0.0, true, nil
			case sqlite.TypeText:
				return "", true, nil
			case sqlite.TypeInteger:
				return 0, true, nil
			default:
				return nil, false, fmt.Errorf("unsupported sqlite data type: %s", colDef.Type)
			}
		}

		return nil, true, nil
	}

	for _, fn := range hooks {
		res, end, err := fn(colDef, valType, val)
		if err != nil {
			return res, false, err
		}

		if end {
			return res, true, nil
		}
	}

	return nil, false, nil
}

// DefaultEncodeConfig holds the default encoding configuration.
var DefaultEncodeConfig = EncodeConfig{
	EncodeHooks: []EncodeFunc{
		DatetimeEncoder(time.UTC),
	},
}
