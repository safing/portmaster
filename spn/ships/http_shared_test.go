package ships

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSharedHTTP(t *testing.T) { //nolint:paralleltest // Test checks global state.
	_, err := New(struct{}{})
	if err != nil {
		t.Errorf("failed to create module ships: %s", err)
	}

	const testPort = 65100

	// Register multiple handlers.
	err = addHTTPHandler(testPort, "", ServeInfoPage)
	require.NoError(t, err, "should be able to share http listener")
	err = addHTTPHandler(testPort, "/test", ServeInfoPage)
	require.NoError(t, err, "should be able to share http listener")
	err = addHTTPHandler(testPort, "/test2", ServeInfoPage)
	require.NoError(t, err, "should be able to share http listener")
	err = addHTTPHandler(testPort, "/", ServeInfoPage)
	require.Error(t, err, "should fail to register path twice")

	// Unregister
	require.NoError(t, removeHTTPHandler(testPort, ""))
	require.NoError(t, removeHTTPHandler(testPort, "/test"))
	require.NoError(t, removeHTTPHandler(testPort, "/not-registered")) // removing unregistered handler does not error
	require.NoError(t, removeHTTPHandler(testPort, "/test2"))
	require.NoError(t, removeHTTPHandler(testPort, "/not-registered")) // removing unregistered handler does not error

	// Check if all handlers are gone again.
	sharedHTTPServersLock.Lock()
	defer sharedHTTPServersLock.Unlock()
	assert.Empty(t, sharedHTTPServers, "shared http handlers should be back to zero")
}
