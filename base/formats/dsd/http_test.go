package dsd

import (
	"mime"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMimeTypes(t *testing.T) {
	t.Parallel()

	// Test static maps.
	for _, mimeType := range FormatToMimeType {
		cleaned, _, err := mime.ParseMediaType(mimeType)
		assert.NoError(t, err, "mime type must be parse-able")
		assert.Equal(t, mimeType, cleaned, "mime type should be clean in map already")
	}
	for mimeType := range MimeTypeToFormat {
		cleaned, _, err := mime.ParseMediaType(mimeType)
		assert.NoError(t, err, "mime type must be parse-able")
		assert.Equal(t, mimeType, cleaned, "mime type should be clean in map already")
	}

	// Test assumptions.
	for accept, format := range map[string]uint8{
		"application/json, image/webp":       JSON,
		"image/webp, application/json":       JSON,
		"application/json;q=0.9, image/webp": JSON,
		"*":                                  DefaultSerializationFormat,
		"*/*":                                DefaultSerializationFormat,
		"text/yAMl":                          YAML,
		" * , yaml ":                         YAML,
		"yaml;charset ,*":                    YAML,
		"xml,*":                              DefaultSerializationFormat,
		"text/xml, text/other":               AUTO,
		"text/*":                             DefaultSerializationFormat,
		"yaml ;charset":                      AUTO, // Invalid mimetype format.
		"":                                   DefaultSerializationFormat,
		"x":                                  AUTO,
	} {
		derivedFormat := FormatFromAccept(accept)
		assert.Equal(t, format, derivedFormat, "assumption for %q should hold", accept)
	}
}
