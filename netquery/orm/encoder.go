package orm

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"zombiezen.com/go/sqlite"
)

type (
	EncodeFunc func(col *ColumnDef, valType reflect.Type, val reflect.Value) (interface{}, bool, error)

	EncodeConfig struct {
		EncodeHooks []EncodeFunc
	}
)

// ToParamMap returns a map that contains the sqlite compatible value of each struct field of
// r using the sqlite column name as a map key. It either uses the name of the
// exported struct field or the value of the "sqlite" tag.
func ToParamMap(ctx context.Context, r interface{}, keyPrefix string, cfg EncodeConfig) (map[string]interface{}, error) {
	// make sure we work on a struct type
	val := reflect.Indirect(reflect.ValueOf(r))
	if val.Kind() != reflect.Struct {
		return nil, fmt.Errorf("%w, got %T", errStructExpected, r)
	}

	res := make(map[string]interface{}, val.NumField())

	for i := 0; i < val.NumField(); i++ {
		fieldType := val.Type().Field(i)
		field := val.Field(i)

		// skip unexported fields
		if !fieldType.IsExported() {
			continue
		}

		colDef, err := getColumnDef(fieldType)
		if err != nil {
			return nil, fmt.Errorf("failed to get column definition for %s: %w", fieldType.Name, err)
		}

		x, found, err := runEncodeHooks(
			colDef,
			fieldType.Type,
			field,
			append(
				cfg.EncodeHooks,
				encodeBasic(),
			),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to run encode hooks: %w", err)
		}

		if !found {
			if reflect.Indirect(field).IsValid() {
				x = reflect.Indirect(field).Interface()
			}
		}

		res[keyPrefix+sqlColumnName(fieldType)] = x

	}

	return res, nil
}

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
					//return reflect.New(valType).Elem().Interface(), true, nil
					panic("nil pointer for not-null field")
				}

				return nil, true, nil
			}

			val = val.Elem()
		}

		switch normalizeKind(kind) {
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

func DatetimeEncoder(loc *time.Location) EncodeFunc {
	return func(colDef *ColumnDef, valType reflect.Type, val reflect.Value) (interface{}, bool, error) {
		// if fieldType holds a pointer we need to dereference the value
		ft := valType.String()
		if valType.Kind() == reflect.Ptr {
			ft = valType.Elem().String()
			val = reflect.Indirect(val)
		}

		// we only care about "time.Time" here
		var t time.Time
		if ft == "time.Time" {
			// handle the zero time as a NULL.
			if !val.IsValid() || val.IsZero() {
				return nil, true, nil
			}

			var ok bool
			valInterface := val.Interface()
			t, ok = valInterface.(time.Time)
			if !ok {
				return nil, false, fmt.Errorf("cannot convert reflect value to time.Time")
			}

		} else if valType.Kind() == reflect.String && colDef.IsTime {
			var err error
			t, err = time.Parse(time.RFC3339, val.String())
			if err != nil {
				return nil, false, fmt.Errorf("failed to parse time as RFC3339: %w", err)
			}

		} else {
			// we don't care ...
			return nil, false, nil
		}

		switch colDef.Type {
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
			switch colDef.Type {
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

var DefaultEncodeConfig = EncodeConfig{
	EncodeHooks: []EncodeFunc{
		DatetimeEncoder(time.UTC),
	},
}
