package ships

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSharedHTTP(t *testing.T) { //nolint:paralleltest // Test checks global state.
	const testPort = 65100

	// Register multiple handlers.
	err := addHTTPHandler(testPort, "", ServeInfoPage)
	assert.NoError(t, err, "should be able to share http listener")
	err = addHTTPHandler(testPort, "/test", ServeInfoPage)
	assert.NoError(t, err, "should be able to share http listener")
	err = addHTTPHandler(testPort, "/test2", ServeInfoPage)
	assert.NoError(t, err, "should be able to share http listener")
	err = addHTTPHandler(testPort, "/", ServeInfoPage)
	assert.Error(t, err, "should fail to register path twice")

	// Unregister
	assert.NoError(t, removeHTTPHandler(testPort, ""))
	assert.NoError(t, removeHTTPHandler(testPort, "/test"))
	assert.NoError(t, removeHTTPHandler(testPort, "/not-registered")) // removing unregistered handler does not error
	assert.NoError(t, removeHTTPHandler(testPort, "/test2"))
	assert.NoError(t, removeHTTPHandler(testPort, "/not-registered")) // removing unregistered handler does not error

	// Check if all handlers are gone again.
	sharedHTTPServersLock.Lock()
	defer sharedHTTPServersLock.Unlock()
	assert.Equal(t, 0, len(sharedHTTPServers), "shared http handlers should be back to zero")
}
