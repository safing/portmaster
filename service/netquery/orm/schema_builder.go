package orm

import (
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"zombiezen.com/go/sqlite"

	"github.com/safing/portmaster/base/log"
)

var errSkipStructField = errors.New("struct field should be skipped")

// Struct Tags.
var (
	TagUnixNano          = "unixnano"
	TagPrimaryKey        = "primary"
	TagAutoIncrement     = "autoincrement"
	TagTime              = "time"
	TagNotNull           = "not-null"
	TagNullable          = "nullable"
	TagTypeInt           = "integer"
	TagTypeText          = "text"
	TagTypePrefixVarchar = "varchar"
	TagTypeBlob          = "blob"
	TagTypeFloat         = "float"
	TagTypePrefixDefault = "default="
)

var sqlTypeMap = map[sqlite.ColumnType]string{
	sqlite.TypeBlob:    "BLOB",
	sqlite.TypeFloat:   "REAL",
	sqlite.TypeInteger: "INTEGER",
	sqlite.TypeText:    "TEXT",
}

type (
	// TableSchema defines a SQL table schema.
	TableSchema struct {
		Name    string
		Columns []ColumnDef
	}

	// ColumnDef defines a SQL column.
	ColumnDef struct { //nolint:maligned
		Name          string
		Nullable      bool
		Type          sqlite.ColumnType
		GoType        reflect.Type
		Length        int
		PrimaryKey    bool
		AutoIncrement bool
		UnixNano      bool
		IsTime        bool
		Default       any
	}
)

// GetColumnDef returns the column definition with the given name.
func (ts TableSchema) GetColumnDef(name string) *ColumnDef {
	for _, def := range ts.Columns {
		if def.Name == name {
			return &def
		}
	}
	return nil
}

// CreateStatement build the CREATE SQL statement for the table.
func (ts TableSchema) CreateStatement(databaseName string, ifNotExists bool) string {
	sql := "CREATE TABLE"
	if ifNotExists {
		sql += " IF NOT EXISTS"
	}
	name := ts.Name
	if databaseName != "" {
		name = databaseName + "." + ts.Name
	}

	sql += " " + name + " ( "

	for idx, col := range ts.Columns {
		sql += col.AsSQL()
		if idx < len(ts.Columns)-1 {
			sql += ", "
		}
	}

	sql += " );"
	return sql
}

// AsSQL builds the SQL column definition.
func (def ColumnDef) AsSQL() string {
	sql := def.Name + " "

	if def.Type == sqlite.TypeText && def.Length > 0 {
		sql += fmt.Sprintf("VARCHAR(%d)", def.Length)
	} else {
		sql += sqlTypeMap[def.Type]
	}

	if def.PrimaryKey {
		sql += " PRIMARY KEY"
	}
	if def.AutoIncrement {
		sql += " AUTOINCREMENT"
	}
	if def.Default != nil {
		sql += " DEFAULT "
		switch def.Type { //nolint:exhaustive // TODO: handle types BLOB, NULL?
		case sqlite.TypeFloat:
			sql += strconv.FormatFloat(def.Default.(float64), 'b', 0, 64) //nolint:forcetypeassert
		case sqlite.TypeInteger:
			sql += strconv.FormatInt(def.Default.(int64), 10) //nolint:forcetypeassert
		case sqlite.TypeText:
			sql += fmt.Sprintf("%q", def.Default.(string)) //nolint:forcetypeassert
		default:
			log.Errorf("unsupported default value: %q %q", def.Type, def.Default)
			sql = strings.TrimSuffix(sql, " DEFAULT ")
		}
		sql += " "
	}
	if !def.Nullable {
		sql += " NOT NULL"
	}

	return sql
}

// GenerateTableSchema generates a table schema from the given struct.
func GenerateTableSchema(name string, d interface{}) (*TableSchema, error) {
	ts := &TableSchema{
		Name: name,
	}

	val := reflect.Indirect(reflect.ValueOf(d))
	if val.Kind() != reflect.Struct {
		return nil, fmt.Errorf("%w, got %T", errStructExpected, d)
	}

	for i := range val.NumField() {
		fieldType := val.Type().Field(i)
		if !fieldType.IsExported() {
			continue
		}

		def, err := getColumnDef(fieldType)
		if err != nil {
			if errors.Is(err, errSkipStructField) {
				continue
			}

			return nil, fmt.Errorf("struct field %s: %w", fieldType.Name, err)
		}

		ts.Columns = append(ts.Columns, *def)
	}

	return ts, nil
}

func getColumnDef(fieldType reflect.StructField) (*ColumnDef, error) {
	def := &ColumnDef{
		Name:     fieldType.Name,
		Nullable: fieldType.Type.Kind() == reflect.Ptr,
	}

	ft := fieldType.Type

	if fieldType.Type.Kind() == reflect.Ptr {
		ft = fieldType.Type.Elem()
	}

	def.GoType = ft
	kind := NormalizeKind(ft.Kind())

	switch kind { //nolint:exhaustive
	case reflect.Int, reflect.Uint:
		def.Type = sqlite.TypeInteger

	case reflect.Float64:
		def.Type = sqlite.TypeFloat

	case reflect.String:
		def.Type = sqlite.TypeText

	case reflect.Slice:
		// only []byte/[]uint8 is supported
		if ft.Elem().Kind() != reflect.Uint8 {
			return nil, fmt.Errorf("slices of type %s is not supported", ft.Elem())
		}

		def.Type = sqlite.TypeBlob
	}

	if err := applyStructFieldTag(fieldType, def); err != nil {
		return nil, err
	}

	return def, nil
}

// applyStructFieldTag parses the sqlite:"" struct field tag and update the column
// definition def accordingly.
func applyStructFieldTag(fieldType reflect.StructField, def *ColumnDef) error {
	parts := strings.Split(fieldType.Tag.Get("sqlite"), ",")
	if len(parts) > 0 && parts[0] != "" {
		if parts[0] == "-" {
			return errSkipStructField
		}

		def.Name = parts[0]
	}

	if len(parts) > 1 {
		for _, k := range parts[1:] {
			switch k {
			// column modifiers
			case TagPrimaryKey:
				def.PrimaryKey = true
			case TagAutoIncrement:
				def.AutoIncrement = true
			case TagNotNull:
				def.Nullable = false
			case TagNullable:
				def.Nullable = true
			case TagUnixNano:
				def.UnixNano = true
			case TagTime:
				def.IsTime = true

			// basic column types
			case TagTypeInt:
				def.Type = sqlite.TypeInteger
			case TagTypeText:
				def.Type = sqlite.TypeText
			case TagTypeFloat:
				def.Type = sqlite.TypeFloat
			case TagTypeBlob:
				def.Type = sqlite.TypeBlob

			// advanced column types
			default:
				if strings.HasPrefix(k, TagTypePrefixVarchar) {
					lenStr := strings.TrimSuffix(strings.TrimPrefix(k, TagTypePrefixVarchar+"("), ")")
					length, err := strconv.ParseInt(lenStr, 10, 0)
					if err != nil {
						return fmt.Errorf("failed to parse varchar length %q: %w", lenStr, err)
					}

					def.Type = sqlite.TypeText
					def.Length = int(length)
				}

				if strings.HasPrefix(k, TagTypePrefixDefault) {
					defaultValue := strings.TrimPrefix(k, TagTypePrefixDefault)
					switch def.Type { //nolint:exhaustive
					case sqlite.TypeFloat:
						fv, err := strconv.ParseFloat(defaultValue, 64)
						if err != nil {
							return fmt.Errorf("failed to parse default value as float %q: %w", defaultValue, err)
						}
						def.Default = fv
					case sqlite.TypeInteger:
						fv, err := strconv.ParseInt(defaultValue, 10, 0)
						if err != nil {
							return fmt.Errorf("failed to parse default value as int %q: %w", defaultValue, err)
						}
						def.Default = fv
					case sqlite.TypeText:
						def.Default = defaultValue
					case sqlite.TypeBlob:
						return errors.New("default values for TypeBlob not yet supported")
					default:
						return fmt.Errorf("failed to apply default value for unknown sqlite column type %s", def.Type)
					}
				}

			}
		}
	}

	return nil
}
