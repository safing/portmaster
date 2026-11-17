package apprise

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSendErrorWithEndlessBody(t *testing.T) {
	t.Parallel()

	// Server that responds with an error and a body that never ends.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		chunk := []byte(strings.Repeat("x", 4096))
		for r.Context().Err() == nil {
			_, err := w.Write(chunk)
			if err != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)

	n := &Notifier{
		URL:           srv.URL,
		AllowUntagged: true,
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	errs := make(chan error, 1)
	go func() {
		errs <- n.Send(ctx, &Message{Body: "test"})
	}()

	select {
	case err := <-errs:
		if err == nil {
			t.Fatal("expected an error")
		}
		if !strings.Contains(err.Error(), "500") {
			t.Fatalf("unexpected error: %s", err)
		}
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("Send did not return: error response body is read without limit")
	}
}

func TestSendErrorMessage(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"no such tag"}`))
	}))
	t.Cleanup(srv.Close)

	n := &Notifier{URL: srv.URL, DefaultTag: "tag"}
	err := n.Send(context.Background(), &Message{Body: "test"})
	if err == nil || !strings.Contains(err.Error(), "no such tag") {
		t.Fatalf("unexpected error: %v", err)
	}
}
