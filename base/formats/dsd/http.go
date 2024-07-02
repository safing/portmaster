package dsd

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// HTTP Related Errors.
var (
	ErrMissingBody        = errors.New("dsd: missing http body")
	ErrMissingContentType = errors.New("dsd: missing http content type")
)

const (
	httpHeaderContentType = "Content-Type"
)

// LoadFromHTTPRequest loads the data from the body into the given interface.
func LoadFromHTTPRequest(r *http.Request, t interface{}) (format uint8, err error) {
	return loadFromHTTP(r.Body, r.Header.Get(httpHeaderContentType), t)
}

// LoadFromHTTPResponse loads the data from the body into the given interface.
// Closing the body is left to the caller.
func LoadFromHTTPResponse(resp *http.Response, t interface{}) (format uint8, err error) {
	return loadFromHTTP(resp.Body, resp.Header.Get(httpHeaderContentType), t)
}

func loadFromHTTP(body io.Reader, mimeType string, t interface{}) (format uint8, err error) {
	// Read full body.
	data, err := io.ReadAll(body)
	if err != nil {
		return 0, fmt.Errorf("dsd: failed to read http body: %w", err)
	}

	// Load depending on mime type.
	return MimeLoad(data, mimeType, t)
}

// RequestHTTPResponseFormat sets the Accept header to the given format.
func RequestHTTPResponseFormat(r *http.Request, format uint8) (mimeType string, err error) {
	// Get mime type.
	mimeType, ok := FormatToMimeType[format]
	if !ok {
		return "", ErrIncompatibleFormat
	}

	// Request response format.
	r.Header.Set("Accept", mimeType)

	return mimeType, nil
}

// DumpToHTTPRequest dumps the given data to the HTTP request using the given
// format. It also sets the Accept header to the same format.
func DumpToHTTPRequest(r *http.Request, t interface{}, format uint8) error {
	// Get mime type and set request format.
	mimeType, err := RequestHTTPResponseFormat(r, format)
	if err != nil {
		return err
	}

	// Serialize data.
	data, err := dumpWithoutIdentifier(t, format, "")
	if err != nil {
		return fmt.Errorf("dsd: failed to serialize: %w", err)
	}

	// Add data to request.
	r.Header.Set("Content-Type", mimeType)
	r.Body = io.NopCloser(bytes.NewReader(data))

	return nil
}

// DumpToHTTPResponse dumpts the given data to the HTTP response, using the
// format defined in the request's Accept header.
func DumpToHTTPResponse(w http.ResponseWriter, r *http.Request, t interface{}) error {
	// Serialize data based on accept header.
	data, mimeType, _, err := MimeDump(t, r.Header.Get("Accept"))
	if err != nil {
		return fmt.Errorf("dsd: failed to serialize: %w", err)
	}

	// Write data to response
	w.Header().Set("Content-Type", mimeType)
	_, err = w.Write(data)
	if err != nil {
		return fmt.Errorf("dsd: failed to write response: %w", err)
	}
	return nil
}

// MimeLoad loads the given data into the interface based on the given mime type accept header.
func MimeLoad(data []byte, accept string, t interface{}) (format uint8, err error) {
	// Find format.
	format = FormatFromAccept(accept)
	if format == 0 {
		return 0, ErrIncompatibleFormat
	}

	// Load data.
	err = LoadAsFormat(data, format, t)
	return format, err
}

// MimeDump dumps the given interface based on the given mime type accept header.
func MimeDump(t any, accept string) (data []byte, mimeType string, format uint8, err error) {
	// Find format.
	format = FormatFromAccept(accept)
	if format == AUTO {
		return nil, "", 0, ErrIncompatibleFormat
	}

	// Serialize and return.
	data, err = dumpWithoutIdentifier(t, format, "")
	return data, mimeType, format, err
}

// FormatFromAccept returns the format for the given accept definition.
// The accept parameter matches the format of the HTTP Accept header.
// Special cases, in this order:
// - If accept is an empty string: returns default serialization format.
// - If accept contains no supported format, but a wildcard: returns default serialization format.
// - If accept contains no supported format, and no wildcard: returns AUTO format.
func FormatFromAccept(accept string) (format uint8) {
	if accept == "" {
		return DefaultSerializationFormat
	}

	var foundWildcard bool
	for _, mimeType := range strings.Split(accept, ",") {
		// Clean mime type.
		mimeType = strings.TrimSpace(mimeType)
		mimeType, _, _ = strings.Cut(mimeType, ";")
		if strings.Contains(mimeType, "/") {
			_, mimeType, _ = strings.Cut(mimeType, "/")
		}
		mimeType = strings.ToLower(mimeType)

		// Check if mime type is supported.
		format, ok := MimeTypeToFormat[mimeType]
		if ok {
			return format
		}

		// Return default mime type as fallback if any mimetype is okay.
		if mimeType == "*" {
			foundWildcard = true
		}
	}

	if foundWildcard {
		return DefaultSerializationFormat
	}
	return AUTO
}

// Format and MimeType mappings.
var (
	FormatToMimeType = map[uint8]string{
		CBOR:    "application/cbor",
		JSON:    "application/json",
		MsgPack: "application/msgpack",
		YAML:    "application/yaml",
	}
	MimeTypeToFormat = map[string]uint8{
		"cbor":    CBOR,
		"json":    JSON,
		"msgpack": MsgPack,
		"yaml":    YAML,
		"yml":     YAML,
	}
)
