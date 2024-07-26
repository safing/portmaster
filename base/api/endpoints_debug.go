package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"runtime/pprof"
	"strings"
	"time"

	"github.com/safing/portmaster/base/info"
	"github.com/safing/portmaster/base/utils/debug"
)

func registerDebugEndpoints() error {
	if err := RegisterEndpoint(Endpoint{
		Path:        "ping",
		Read:        PermitAnyone,
		ActionFunc:  ping,
		Name:        "Ping",
		Description: "Pong.",
	}); err != nil {
		return err
	}

	if err := RegisterEndpoint(Endpoint{
		Path:        "ready",
		Read:        PermitAnyone,
		ActionFunc:  ready,
		Name:        "Ready",
		Description: "Check if Portmaster has completed starting and is ready.",
	}); err != nil {
		return err
	}

	if err := RegisterEndpoint(Endpoint{
		Path:        "debug/stack",
		Read:        PermitAnyone,
		DataFunc:    getStack,
		Name:        "Get Goroutine Stack",
		Description: "Returns the current goroutine stack.",
	}); err != nil {
		return err
	}

	if err := RegisterEndpoint(Endpoint{
		Path:        "debug/stack/print",
		Read:        PermitAnyone,
		ActionFunc:  printStack,
		Name:        "Print Goroutine Stack",
		Description: "Prints the current goroutine stack to stdout.",
	}); err != nil {
		return err
	}

	if err := RegisterEndpoint(Endpoint{
		Path:     "debug/cpu",
		MimeType: "application/octet-stream",
		Read:     PermitAnyone,
		DataFunc: handleCPUProfile,
		Name:     "Get CPU Profile",
		Description: strings.ReplaceAll(`Gather and return the CPU profile.
This data needs to gathered over a period of time, which is specified using the duration parameter.

You can easily view this data in your browser with this command (with Go installed):
"go tool pprof -http :8888 http://127.0.0.1:817/api/v1/debug/cpu"
`, `"`, "`"),
		Parameters: []Parameter{{
			Method:      http.MethodGet,
			Field:       "duration",
			Value:       "10s",
			Description: "Specify the formatting style. The default is simple markdown formatting.",
		}},
	}); err != nil {
		return err
	}

	if err := RegisterEndpoint(Endpoint{
		Path:     "debug/heap",
		MimeType: "application/octet-stream",
		Read:     PermitAnyone,
		DataFunc: handleHeapProfile,
		Name:     "Get Heap Profile",
		Description: strings.ReplaceAll(`Gather and return the heap memory profile.
		
		You can easily view this data in your browser with this command (with Go installed):
		"go tool pprof -http :8888 http://127.0.0.1:817/api/v1/debug/heap"
		`, `"`, "`"),
	}); err != nil {
		return err
	}

	if err := RegisterEndpoint(Endpoint{
		Path:     "debug/allocs",
		MimeType: "application/octet-stream",
		Read:     PermitAnyone,
		DataFunc: handleAllocsProfile,
		Name:     "Get Allocs Profile",
		Description: strings.ReplaceAll(`Gather and return the memory allocation profile.
		
		You can easily view this data in your browser with this command (with Go installed):
		"go tool pprof -http :8888 http://127.0.0.1:817/api/v1/debug/allocs"
		`, `"`, "`"),
	}); err != nil {
		return err
	}

	if err := RegisterEndpoint(Endpoint{
		Path:        "debug/info",
		Read:        PermitAnyone,
		DataFunc:    debugInfo,
		Name:        "Get Debug Information",
		Description: "Returns debugging information, including the version and platform info, errors, logs and the current goroutine stack.",
		Parameters: []Parameter{{
			Method:      http.MethodGet,
			Field:       "style",
			Value:       "github",
			Description: "Specify the formatting style. The default is simple markdown formatting.",
		}},
	}); err != nil {
		return err
	}

	return nil
}

// ping responds with pong.
func ping(ar *Request) (msg string, err error) {
	return "Pong.", nil
}

// ready checks if Portmaster has completed starting.
func ready(ar *Request) (msg string, err error) {
	if module.instance.Ready() {
		return "", ErrorWithStatus(errors.New("portmaster is not ready, reload (F5) to try again"), http.StatusTooEarly)
	}
	return "Portmaster is ready.", nil
}

// getStack returns the current goroutine stack.
func getStack(_ *Request) (data []byte, err error) {
	buf := &bytes.Buffer{}
	err = pprof.Lookup("goroutine").WriteTo(buf, 1)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// printStack prints the current goroutine stack to stderr.
func printStack(_ *Request) (msg string, err error) {
	_, err = fmt.Fprint(os.Stderr, "===== PRINTING STACK =====\n")
	if err == nil {
		err = pprof.Lookup("goroutine").WriteTo(os.Stderr, 1)
	}
	if err == nil {
		_, err = fmt.Fprint(os.Stderr, "===== END OF STACK =====\n")
	}
	if err != nil {
		return "", err
	}
	return "stack printed to stdout", nil
}

// handleCPUProfile returns the CPU profile.
func handleCPUProfile(ar *Request) (data []byte, err error) {
	// Parse duration.
	duration := 10 * time.Second
	if durationOption := ar.Request.URL.Query().Get("duration"); durationOption != "" {
		parsedDuration, err := time.ParseDuration(durationOption)
		if err != nil {
			return nil, fmt.Errorf("failed to parse duration: %w", err)
		}
		duration = parsedDuration
	}

	// Indicate download and filename.
	ar.ResponseHeader.Set(
		"Content-Disposition",
		fmt.Sprintf(`attachment; filename="portmaster-cpu-profile_v%s.pprof"`, info.Version()),
	)

	// Start CPU profiling.
	buf := new(bytes.Buffer)
	if err := pprof.StartCPUProfile(buf); err != nil {
		return nil, fmt.Errorf("failed to start cpu profile: %w", err)
	}

	// Wait for the specified duration.
	select {
	case <-time.After(duration):
	case <-ar.Context().Done():
		pprof.StopCPUProfile()
		return nil, context.Canceled
	}

	// Stop CPU profiling and return data.
	pprof.StopCPUProfile()
	return buf.Bytes(), nil
}

// handleHeapProfile returns the Heap profile.
func handleHeapProfile(ar *Request) (data []byte, err error) {
	// Indicate download and filename.
	ar.ResponseHeader.Set(
		"Content-Disposition",
		fmt.Sprintf(`attachment; filename="portmaster-memory-heap-profile_v%s.pprof"`, info.Version()),
	)

	buf := new(bytes.Buffer)
	if err := pprof.Lookup("heap").WriteTo(buf, 0); err != nil {
		return nil, fmt.Errorf("failed to write heap profile: %w", err)
	}
	return buf.Bytes(), nil
}

// handleAllocsProfile returns the Allocs profile.
func handleAllocsProfile(ar *Request) (data []byte, err error) {
	// Indicate download and filename.
	ar.ResponseHeader.Set(
		"Content-Disposition",
		fmt.Sprintf(`attachment; filename="portmaster-memory-allocs-profile_v%s.pprof"`, info.Version()),
	)

	buf := new(bytes.Buffer)
	if err := pprof.Lookup("allocs").WriteTo(buf, 0); err != nil {
		return nil, fmt.Errorf("failed to write allocs profile: %w", err)
	}
	return buf.Bytes(), nil
}

// debugInfo returns the debugging information for support requests.
func debugInfo(ar *Request) (data []byte, err error) {
	// Create debug information helper.
	di := new(debug.Info)
	di.Style = ar.Request.URL.Query().Get("style")

	// Add debug information.
	di.AddVersionInfo()
	di.AddPlatformInfo(ar.Context())
	di.AddLastUnexpectedLogs()
	di.AddGoroutineStack()

	// Return data.
	return di.Bytes(), nil
}
