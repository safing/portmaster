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

// EncodeAsMap returns a map that contains the value of each struct field of
// r using the sqlite column name as a map key. It either uses the name of the
// exported struct field or the value of the "sqlite" tag.
func EncodeAsMap(ctx context.Context, r interface{}, keyPrefix string, cfg EncodeConfig) (map[string]interface{}, error) {
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

		colDev, err := getColumnDef(fieldType)
		if err != nil {
			return nil, fmt.Errorf("failed to get column definition for %s: %w", fieldType.Name, err)
		}

		x, found, err := runEncodeHooks(colDev, fieldType.Type, field, cfg.EncodeHooks)
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

func DatetimeEncoder(loc *time.Location) EncodeFunc {
	return func(colDev *ColumnDef, valType reflect.Type, val reflect.Value) (interface{}, bool, error) {
		// if fieldType holds a pointer we need to dereference the value
		ft := valType.String()
		if valType.Kind() == reflect.Ptr {
			ft = valType.Elem().String()
			val = reflect.Indirect(val)
		}

		// we only care about "time.Time" here
		if ft != "time.Time" {
			return nil, false, nil
		}

		// handle the zero time as a NULL.
		if !val.IsValid() || val.IsZero() {
			return nil, true, nil
		}

		valInterface := val.Interface()
		t, ok := valInterface.(time.Time)
		if !ok {
			return nil, false, fmt.Errorf("cannot convert reflect value to time.Time")
		}

		switch colDev.Type {
		case sqlite.TypeInteger:
			if colDev.UnixNano {
				return t.UnixNano(), true, nil
			}
			return t.Unix(), true, nil
		case sqlite.TypeText:
			str := t.In(loc).Format(sqliteTimeFormat)

			return str, true, nil
		}

		return nil, false, fmt.Errorf("cannot store time.Time in %s", colDev.Type)
	}
}

func runEncodeHooks(colDev *ColumnDef, valType reflect.Type, val reflect.Value, hooks []EncodeFunc) (interface{}, bool, error) {
	for _, fn := range hooks {
		res, end, err := fn(colDev, valType, val)
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
