package ships

import (
	"io"
	"net"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const sharedHTTPTestPort = 65100

// requireSharedHTTPModule creates the module the shared HTTP servers run in,
// unless it already exists from a previous run of the tests.
func requireSharedHTTPModule(t *testing.T) {
	t.Helper()

	if module != nil {
		return
	}
	_, err := New(struct{}{})
	require.NoError(t, err, "failed to create module ships")
}

func TestSharedHTTP(t *testing.T) { //nolint:paralleltest // Test checks global state.
	requireSharedHTTPModule(t)

	// Register multiple handlers.
	err := addHTTPHandler(sharedHTTPTestPort, "", ServeInfoPage)
	require.NoError(t, err, "should be able to share http listener")
	err = addHTTPHandler(sharedHTTPTestPort, "/test", ServeInfoPage)
	require.NoError(t, err, "should be able to share http listener")
	err = addHTTPHandler(sharedHTTPTestPort, "/test2", ServeInfoPage)
	require.NoError(t, err, "should be able to share http listener")
	err = addHTTPHandler(sharedHTTPTestPort, "/", ServeInfoPage)
	require.Error(t, err, "should fail to register path twice")

	// Unregister
	require.NoError(t, removeHTTPHandler(sharedHTTPTestPort, ""))
	require.NoError(t, removeHTTPHandler(sharedHTTPTestPort, "/test"))
	require.NoError(t, removeHTTPHandler(sharedHTTPTestPort, "/not-registered")) // removing unregistered handler does not error

	// The server must keep serving the remaining handler.
	sharedHTTPServersLock.Lock()
	_, ok := sharedHTTPServers[sharedHTTPTestPort]
	sharedHTTPServersLock.Unlock()
	assert.True(t, ok, "shared http server should remain while handlers are registered")

	require.NoError(t, removeHTTPHandler(sharedHTTPTestPort, "/test2"))
	require.NoError(t, removeHTTPHandler(sharedHTTPTestPort, "/not-registered")) // removing unregistered handler does not error

	// Check if all handlers are gone again.
	sharedHTTPServersLock.Lock()
	assert.Empty(t, sharedHTTPServers, "shared http handlers should be back to zero")
	sharedHTTPServersLock.Unlock()

	// The port must be released after the last handler is removed.
	listener, err := net.Listen("tcp", ":"+strconv.Itoa(sharedHTTPTestPort))
	require.NoError(t, err, "port should be released after the last handler is removed")
	_ = listener.Close()
}

// TestSharedHTTPConcurrentRequests checks that a slow handler does not block
// requests to other handlers of the same shared server.
func TestSharedHTTPConcurrentRequests(t *testing.T) { //nolint:paralleltest // Test checks global state.
	requireSharedHTTPModule(t)

	release := make(chan struct{})
	slowStarted := make(chan struct{})
	err := addHTTPHandler(sharedHTTPTestPort, "/slow", func(w http.ResponseWriter, _ *http.Request) {
		close(slowStarted)
		<-release
		w.WriteHeader(http.StatusOK)
	})
	require.NoError(t, err)
	err = addHTTPHandler(sharedHTTPTestPort, "/fast", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	require.NoError(t, err)

	baseURL := "http://127.0.0.1:" + strconv.Itoa(sharedHTTPTestPort)
	client := &http.Client{Timeout: 5 * time.Second}

	// Start the slow request and wait until its handler runs.
	slowDone := make(chan error, 1)
	go func() {
		resp, err := client.Get(baseURL + "/slow") //nolint:noctx // Client has a timeout.
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}
		slowDone <- err
	}()
	select {
	case <-slowStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("slow handler did not start")
	}

	// The fast request must complete while the slow one is still running.
	fastClient := &http.Client{Timeout: 2 * time.Second}
	resp, err := fastClient.Get(baseURL + "/fast") //nolint:noctx // Client has a timeout.
	if err == nil {
		_ = resp.Body.Close()
	}
	close(release)
	require.NoError(t, err, "request to another handler should not wait for the slow handler")
	require.NoError(t, <-slowDone)

	require.NoError(t, removeHTTPHandler(sharedHTTPTestPort, "/slow"))
	require.NoError(t, removeHTTPHandler(sharedHTTPTestPort, "/fast"))
}
