package record

import (
	"strings"
)

// ParseKey splits a key into it's database name and key parts.
func ParseKey(key string) (dbName, dbKey string) {
	splitted := strings.SplitN(key, ":", 2)
	if len(splitted) < 2 {
		return splitted[0], ""
	}
	return splitted[0], strings.Join(splitted[1:], ":")
}
