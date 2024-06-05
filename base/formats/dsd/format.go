package dsd

import "errors"

// Errors.
var (
	ErrIncompatibleFormat = errors.New("dsd: format is incompatible with operation")
	ErrIsRaw              = errors.New("dsd: given data is in raw format")
	ErrUnknownFormat      = errors.New("dsd: format is unknown")
)

// Format types.
const (
	AUTO = 0

	// Serialization types.
	RAW     = 1
	CBOR    = 67 // C
	GenCode = 71 // G
	JSON    = 74 // J
	MsgPack = 77 // M
	YAML    = 89 // Y

	// Compression types.
	GZIP = 90 // Z

	// Special types.
	LIST = 76 // L
)

// Default Formats.
var (
	DefaultSerializationFormat uint8 = JSON
	DefaultCompressionFormat   uint8 = GZIP
)

// ValidateSerializationFormat validates if the format is for serialization,
// and returns the validated format as well as the result of the validation.
// If called on the AUTO format, it returns the default serialization format.
func ValidateSerializationFormat(format uint8) (validatedFormat uint8, ok bool) {
	switch format {
	case AUTO:
		return DefaultSerializationFormat, true
	case RAW:
		return format, true
	case CBOR:
		return format, true
	case GenCode:
		return format, true
	case JSON:
		return format, true
	case YAML:
		return format, true
	case MsgPack:
		return format, true
	default:
		return 0, false
	}
}

// ValidateCompressionFormat validates if the format is for compression,
// and returns the validated format as well as the result of the validation.
// If called on the AUTO format, it returns the default compression format.
func ValidateCompressionFormat(format uint8) (validatedFormat uint8, ok bool) {
	switch format {
	case AUTO:
		return DefaultCompressionFormat, true
	case GZIP:
		return format, true
	default:
		return 0, false
	}
}
