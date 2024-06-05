package dsd

// dynamic structured data
// check here for some benchmarks: https://github.com/alecthomas/go_serialization_benchmarks

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/fxamacker/cbor/v2"
	"github.com/ghodss/yaml"
	"github.com/vmihailenco/msgpack/v5"

	"github.com/safing/portmaster/base/formats/varint"
	"github.com/safing/portmaster/base/utils"
)

// Load loads an dsd structured data blob into the given interface.
func Load(data []byte, t interface{}) (format uint8, err error) {
	format, read, err := loadFormat(data)
	if err != nil {
		return 0, err
	}

	_, ok := ValidateSerializationFormat(format)
	if ok {
		return format, LoadAsFormat(data[read:], format, t)
	}
	return DecompressAndLoad(data[read:], format, t)
}

// LoadAsFormat loads a data blob into the interface using the specified format.
func LoadAsFormat(data []byte, format uint8, t interface{}) (err error) {
	switch format {
	case RAW:
		return ErrIsRaw
	case JSON:
		err = json.Unmarshal(data, t)
		if err != nil {
			return fmt.Errorf("dsd: failed to unpack json: %w, data: %s", err, utils.SafeFirst16Bytes(data))
		}
		return nil
	case YAML:
		err = yaml.Unmarshal(data, t)
		if err != nil {
			return fmt.Errorf("dsd: failed to unpack yaml: %w, data: %s", err, utils.SafeFirst16Bytes(data))
		}
		return nil
	case CBOR:
		err = cbor.Unmarshal(data, t)
		if err != nil {
			return fmt.Errorf("dsd: failed to unpack cbor: %w, data: %s", err, utils.SafeFirst16Bytes(data))
		}
		return nil
	case MsgPack:
		err = msgpack.Unmarshal(data, t)
		if err != nil {
			return fmt.Errorf("dsd: failed to unpack msgpack: %w, data: %s", err, utils.SafeFirst16Bytes(data))
		}
		return nil
	case GenCode:
		genCodeStruct, ok := t.(GenCodeCompatible)
		if !ok {
			return errors.New("dsd: gencode is not supported by the given data structure")
		}
		_, err = genCodeStruct.GenCodeUnmarshal(data)
		if err != nil {
			return fmt.Errorf("dsd: failed to unpack gencode: %w, data: %s", err, utils.SafeFirst16Bytes(data))
		}
		return nil
	default:
		return ErrIncompatibleFormat
	}
}

func loadFormat(data []byte) (format uint8, read int, err error) {
	format, read, err = varint.Unpack8(data)
	if err != nil {
		return 0, 0, err
	}
	if len(data) <= read {
		return 0, 0, io.ErrUnexpectedEOF
	}

	return format, read, nil
}

// Dump stores the interface as a dsd formatted data structure.
func Dump(t interface{}, format uint8) ([]byte, error) {
	return DumpIndent(t, format, "")
}

// DumpIndent stores the interface as a dsd formatted data structure with indentation, if available.
func DumpIndent(t interface{}, format uint8, indent string) ([]byte, error) {
	data, err := dumpWithoutIdentifier(t, format, indent)
	if err != nil {
		return nil, err
	}

	// TODO: Find a better way to do this.
	return append(varint.Pack8(format), data...), nil
}

func dumpWithoutIdentifier(t interface{}, format uint8, indent string) ([]byte, error) {
	format, ok := ValidateSerializationFormat(format)
	if !ok {
		return nil, ErrIncompatibleFormat
	}

	var data []byte
	var err error
	switch format {
	case RAW:
		var ok bool
		data, ok = t.([]byte)
		if !ok {
			return nil, ErrIncompatibleFormat
		}
	case JSON:
		// TODO: use SetEscapeHTML(false)
		if indent != "" {
			data, err = json.MarshalIndent(t, "", indent)
		} else {
			data, err = json.Marshal(t)
		}
		if err != nil {
			return nil, err
		}
	case YAML:
		data, err = yaml.Marshal(t)
		if err != nil {
			return nil, err
		}
	case CBOR:
		data, err = cbor.Marshal(t)
		if err != nil {
			return nil, err
		}
	case MsgPack:
		data, err = msgpack.Marshal(t)
		if err != nil {
			return nil, err
		}
	case GenCode:
		genCodeStruct, ok := t.(GenCodeCompatible)
		if !ok {
			return nil, errors.New("dsd: gencode is not supported by the given data structure")
		}
		data, err = genCodeStruct.GenCodeMarshal(nil)
		if err != nil {
			return nil, fmt.Errorf("dsd: failed to pack gencode struct: %w", err)
		}
	default:
		return nil, ErrIncompatibleFormat
	}

	return data, nil
}
