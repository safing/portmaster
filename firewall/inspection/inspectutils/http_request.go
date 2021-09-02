package inspectutils

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/safing/portmaster/network"
)

var HTTPMethods = []string{
	// Normal HTTP methods
	"GET",
	"POST",
	"PUT",
	"PATCH",
	"OPTIONS",
	"HEAD",
	"DELETE",

	// SSDP
	"MS-SEARCH",
	"NOTIFY",

	// WebDAV
	"PROPFIND",
	"MKCOL",
	"RMCOL",
	// TODO(ppacher): possible more for webdav ...
}

var ErrUnexpectedHTTPVerb = errors.New("unexpected HTTP verb")

type HTTPRequestDecoder struct {
	methods  []string
	minBytes int
	l        sync.Mutex
	data     map[string][]byte
}

// NewHTTPRequestDecoder returns a new HTTP request decoder. methods
// may be set to a list of allowed HTTP methods. These list is used
// to early-abort inspecting a TCP stream. If methods is nil the stream
// will be inspected until http.ParseRequests returns an non EOF error.
// To use the default set of methods, pass DefaultMethods here.
func NewHTTPRequestDecoder(methods []string) *HTTPRequestDecoder {
	var minBytes int
	for _, m := range methods {
		if len(m) > minBytes {
			minBytes = len(m)
		}
	}
	return &HTTPRequestDecoder{
		methods:  methods,
		minBytes: minBytes,
		data:     make(map[string][]byte),
	}
}

func (decoder *HTTPRequestDecoder) HandleStream(conn *network.Connection, dir network.FlowDirection, data []byte) (*http.Request, error) {
	decoder.l.Lock()
	defer decoder.l.Unlock()

	decoder.data[conn.ID] = append(decoder.data[conn.ID], data...)
	allData := decoder.data[conn.ID]

	if decoder.methods != nil {
		// we don't even have enough data for a HTTP Verb
		// so nothing todo...
		if len(allData) < decoder.minBytes {
			return nil, nil
		}

		// check if we find one of the requested HTTP methods
		foundMethod := false
		lowercased := strings.ToLower(string(allData))
		for _, m := range decoder.methods {
			if strings.HasPrefix(lowercased, strings.ToLower(m)) {
				foundMethod = true
				break
			}
		}
		if !foundMethod {
			return nil, fmt.Errorf("%w: %s", ErrUnexpectedHTTPVerb, string(allData[:decoder.minBytes]))
		}
	}

	// try to parse a http request
	reader := bufio.NewReader(bytes.NewReader(allData))
	req, err := http.ReadRequest(reader)
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		// we don't have the full request yet ...
		return nil, nil
	}

	// we failed to parse the request so we can safely reset the data here
	delete(decoder.data, conn.ID)

	if err != nil {
		return nil, err
	}
	return req, nil
}
