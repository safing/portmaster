package utils

import (
	"encoding/hex"
	"strings"
)

// SafeFirst16Bytes return the first 16 bytes of the given data in safe form.
func SafeFirst16Bytes(data []byte) string {
	if len(data) == 0 {
		return "<empty>"
	}

	return strings.TrimPrefix(
		strings.SplitN(hex.Dump(data), "\n", 2)[0],
		"00000000  ",
	)
}

// SafeFirst16Chars return the first 16 characters of the given data in safe form.
func SafeFirst16Chars(s string) string {
	return SafeFirst16Bytes([]byte(s))
}
