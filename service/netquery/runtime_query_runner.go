package netquery

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/safing/portmaster/base/database/record"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/runtime"
	"github.com/safing/portmaster/service/netquery/orm"
	"github.com/safing/structures/dsd"
)

// RuntimeQueryRunner provides a simple interface for the runtime database
// that allows direct SQL queries to be performed against db.
// Each resulting row of that query are marshaled as map[string]interface{}
// and returned as a single record to the caller.
//
// Using portbase/database#Query is not possible because portbase/database will
// complain about the SQL query being invalid. To work around that issue,
// RuntimeQueryRunner uses a 'GET key' request where the SQL query is embedded into
// the record key.
type RuntimeQueryRunner struct {
	db        *Database
	reg       *runtime.Registry
	keyPrefix string
}

// NewRuntimeQueryRunner returns a new runtime SQL query runner that parses
// and serves SQL queries form GET <prefix>/<plain sql query> requests.
func NewRuntimeQueryRunner(db *Database, prefix string, reg *runtime.Registry) (*RuntimeQueryRunner, error) {
	runner := &RuntimeQueryRunner{
		db:        db,
		reg:       reg,
		keyPrefix: prefix,
	}

	if _, err := reg.Register(prefix, runtime.SimpleValueGetterFunc(runner.get)); err != nil {
		return nil, fmt.Errorf("failed to register runtime value provider: %w", err)
	}

	return runner, nil
}

func (runner *RuntimeQueryRunner) get(keyOrPrefix string) ([]record.Record, error) {
	query := strings.TrimPrefix(
		keyOrPrefix,
		runner.keyPrefix,
	)

	log.Infof("netquery: executing custom SQL query: %q", query)

	var result []map[string]interface{}
	if err := runner.db.Execute(context.Background(), query, orm.WithResult(&result)); err != nil {
		return nil, fmt.Errorf("failed to perform query %q: %w", query, err)
	}

	// we need to wrap the result slice into a map as portbase/database attempts
	// to inject a _meta field.
	blob, err := json.Marshal(map[string]interface{}{
		"result": result,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal result: %w", err)
	}

	// construct a new record wrapper that uses the already prepared JSON blob.
	key := fmt.Sprintf("%s:%s", runner.reg.DatabaseName(), keyOrPrefix)
	wrapper, err := record.NewWrapper(key, new(record.Meta), dsd.JSON, blob)
	if err != nil {
		return nil, fmt.Errorf("failed to create record wrapper: %w", err)
	}

	return []record.Record{wrapper}, nil
}
