package netquery

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/safing/portmaster/base/database"
	"github.com/safing/portmaster/base/database/query"
	"github.com/safing/portmaster/base/database/record"
	"github.com/safing/portmaster/base/runtime"
)

// TestPushConnUpdate checks that connection updates are still pushed to
// subscribers of the connections, and only created for them.
func TestPushConnUpdate(t *testing.T) { //nolint:paralleltest // uses the package-level module.
	// The database system can only be initialized once per process. Only
	// injected databases are used, so the directory does not matter.
	_ = database.Initialize(t.TempDir())

	// The injected database cannot be withdrawn again: use a new one per run.
	dbName := "netquery-push-test-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	_, err := database.Register(&database.Database{
		Name:        dbName,
		Description: "test",
		StorageType: database.StorageTypeInjected,
	})
	require.NoError(t, err)

	reg := runtime.NewRegistry()
	mng, err := NewManager(newTestDatabase(t), "netquery/conns/", reg)
	require.NoError(t, err)
	require.NoError(t, reg.InjectAsDatabase(dbName))

	conn := newBenchNetworkConn(t, "pushed")
	model, err := convertConnection(conn)
	require.NoError(t, err)
	key := "netquery/conns/" + model.ID

	db := database.NewInterface(&database.Options{Local: true, Internal: true})

	// Subscribers of other keys do not receive connections.
	other, err := db.Subscribe(query.New(dbName + ":other/"))
	require.NoError(t, err)
	assert.False(t, reg.HasSubscribers(key))
	mng.handleConnection(context.Background(), conn)
	select {
	case r := <-other.Feed:
		t.Fatalf("unexpected update: %s", r.Key())
	default:
	}

	// Subscribers of the connections receive the update.
	sub, err := db.Subscribe(query.New(dbName + ":netquery/conns/"))
	require.NoError(t, err)
	assert.True(t, reg.HasSubscribers(key))
	mng.handleConnection(context.Background(), conn)

	select {
	case r := <-sub.Feed:
		assert.Equal(t, dbName+":"+key, r.Key())
		wrapper, ok := r.(*record.Wrapper)
		require.True(t, ok)
		expected, err := json.Marshal(model)
		require.NoError(t, err)
		assert.JSONEq(t, string(expected), string(wrapper.Data))
	case <-time.After(5 * time.Second):
		t.Fatal("update was not pushed to the subscriber")
	}

	// And not anymore after the subscription was canceled.
	require.NoError(t, sub.Cancel())
	require.NoError(t, other.Cancel())
	assert.False(t, reg.HasSubscribers(key))
}
