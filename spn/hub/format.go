package hub

import (
	"fmt"
	"net"
	"regexp"

	"github.com/safing/portmaster/service/network/netutils"
)

// BaselineCharset defines the permitted characters.
var BaselineCharset = regexp.MustCompile(
	// Start of charset selection.
	`^[` +
		// Printable ASCII (character code 32-127), excluding common control characters of different languages: "$%&';<>\` and DELETE.
		` !#()*+,\-\./0-9:=?@A-Z[\]^_a-z{|}~` +
		// Only latin characters from extended ASCII (character code 128-255).
		`ŠŒŽšœžŸ¡¿ÀÁÂÃÄÅÆÇÈÉÊËÌÍÎÏÐÑÒÓÔÕÖØÙÚÛÜÝÞßàáâãäåæçèéêëìíîïðñòóôõöøùúûüýþÿ` +
		// End of charset selection.
		`]*$`,
)

func checkStringFormat(fieldName, value string, maxLength int) error {
	switch {
	case len(value) > maxLength:
		return fmt.Errorf("field %s with length of %d exceeds max length of %d", fieldName, len(value), maxLength)
	case !BaselineCharset.MatchString(value):
		return fmt.Errorf("field %s contains characters not permitted by baseline validation", fieldName)
	default:
		return nil
	}
}

func checkStringSliceFormat(fieldName string, value []string, maxLength, maxStringLength int) error { //nolint:unparam
	if len(value) > maxLength {
		return fmt.Errorf("field %s with array/slice length of %d exceeds max length of %d", fieldName, len(value), maxLength)
	}
	for _, s := range value {
		if err := checkStringFormat(fieldName, s, maxStringLength); err != nil {
			return err
		}
	}
	return nil
}

func checkByteSliceFormat(fieldName string, value []byte, maxLength int) error {
	switch {
	case len(value) > maxLength:
		return fmt.Errorf("field %s with length of %d exceeds max length of %d", fieldName, len(value), maxLength)
	default:
		return nil
	}
}

func checkIPFormat(fieldName string, value net.IP) error {
	// Check if there is an IP address.
	if value == nil {
		return nil
	}

	switch {
	case len(value) != 4 && len(value) != 16:
		return fmt.Errorf("field %s has an invalid length of %d for an IP address", fieldName, len(value))
	case netutils.GetIPScope(value) == netutils.Invalid:
		return fmt.Errorf("field %s holds an invalid IP address: %s", fieldName, value)
	default:
		return nil
	}
}
