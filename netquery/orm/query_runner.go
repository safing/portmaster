package orm

import (
	"context"
	"fmt"
	"reflect"

	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

type (
	QueryOption func(opts *queryOpts)

	queryOpts struct {
		Transient    bool
		Args         []interface{}
		NamedArgs    map[string]interface{}
		Result       interface{}
		DecodeConfig DecodeConfig
	}
)

func WithTransient() QueryOption {
	return func(opts *queryOpts) {
		opts.Transient = true
	}
}

func WithArgs(args ...interface{}) QueryOption {
	return func(opts *queryOpts) {
		opts.Args = args
	}
}

func WithNamedArgs(args map[string]interface{}) QueryOption {
	return func(opts *queryOpts) {
		opts.NamedArgs = args
	}
}

func WithResult(result interface{}) QueryOption {
	return func(opts *queryOpts) {
		opts.Result = result
	}
}

func WithDecodeConfig(cfg DecodeConfig) QueryOption {
	return func(opts *queryOpts) {
		opts.DecodeConfig = cfg
	}
}

func RunQuery(ctx context.Context, conn *sqlite.Conn, sql string, modifiers ...QueryOption) error {
	args := queryOpts{
		DecodeConfig: DefaultDecodeConfig,
	}

	for _, fn := range modifiers {
		fn(&args)
	}

	opts := &sqlitex.ExecOptions{
		Args:  args.Args,
		Named: args.NamedArgs,
	}

	var (
		sliceVal    reflect.Value
		valElemType reflect.Type
	)

	if args.Result != nil {
		target := args.Result
		outVal := reflect.ValueOf(target)
		if outVal.Kind() != reflect.Ptr {
			return fmt.Errorf("target must be a pointer, got %T", target)
		}

		sliceVal = reflect.Indirect(outVal)
		if !sliceVal.IsValid() || sliceVal.IsNil() {
			newVal := reflect.Zero(outVal.Type().Elem())
			sliceVal.Set(newVal)
		}

		kind := sliceVal.Kind()
		if kind != reflect.Slice {
			return fmt.Errorf("target but be pointer to slice, got %T", target)
		}
		valType := sliceVal.Type()
		valElemType = valType.Elem()

		opts.ResultFunc = func(stmt *sqlite.Stmt) error {
			var currentField reflect.Value

			currentField = reflect.New(valElemType)

			if err := DecodeStmt(ctx, stmt, currentField.Interface(), args.DecodeConfig); err != nil {
				return err
			}

			sliceVal = reflect.Append(sliceVal, reflect.Indirect(currentField))

			return nil
		}
	}

	var err error
	if args.Transient {
		err = sqlitex.ExecuteTransient(conn, sql, opts)
	} else {
		err = sqlitex.Execute(conn, sql, opts)
	}
	if err != nil {
		return err
	}

	if args.Result != nil {
		reflect.Indirect(reflect.ValueOf(args.Result)).Set(sliceVal)
	}

	return nil
}
