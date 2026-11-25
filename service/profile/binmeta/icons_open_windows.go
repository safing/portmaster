package binmeta

import "os"

// openIconFile opens path read-only. Named pipes on Windows do not live in
// the icon locations core searches, so no non-blocking flag is needed.
func openIconFile(path string) (*os.File, error) {
	return os.Open(path)
}
