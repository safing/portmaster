package orm

import (
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"zombiezen.com/go/sqlite"
)

var (
	errSkipStructField = errors.New("struct field should be skipped")
)

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
)

var sqlTypeMap = map[sqlite.ColumnType]string{
	sqlite.TypeBlob:    "BLOB",
	sqlite.TypeFloat:   "REAL",
	sqlite.TypeInteger: "INTEGER",
	sqlite.TypeText:    "TEXT",
}

type (
	TableSchema struct {
		Name    string
		Columns []ColumnDef
	}

	ColumnDef struct {
		Name          string
		Nullable      bool
		Type          sqlite.ColumnType
		Length        int
		PrimaryKey    bool
		AutoIncrement bool
		UnixNano      bool
		IsTime        bool
	}
)

func (ts TableSchema) CreateStatement(ifNotExists bool) string {
	sql := "CREATE TABLE"
	if ifNotExists {
		sql += " IF NOT EXISTS"
	}
	sql += " " + ts.Name + " ( "

	for idx, col := range ts.Columns {
		sql += col.AsSQL()
		if idx < len(ts.Columns)-1 {
			sql += ", "
		}
	}

	sql += " );"
	return sql
}

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
	if !def.Nullable {
		sql += " NOT NULL"
	}

	return sql
}

func GenerateTableSchema(name string, d interface{}) (*TableSchema, error) {
	ts := &TableSchema{
		Name: name,
	}

	val := reflect.Indirect(reflect.ValueOf(d))
	if val.Kind() != reflect.Struct {
		return nil, fmt.Errorf("%w, got %T", errStructExpected, d)
	}

	for i := 0; i < val.NumField(); i++ {
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

	kind := normalizeKind(ft.Kind())

	switch kind {
	case reflect.Int:
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
			// column modifieres
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

			}
		}
	}

	return nil
}
