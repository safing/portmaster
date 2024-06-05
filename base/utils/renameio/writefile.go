package renameio

import "os"

// WriteFile mirrors os.WriteFile, replacing an existing file with the same
// name atomically.
func WriteFile(filename string, data []byte, perm os.FileMode) error {
	t, err := TempFile("", filename)
	if err != nil {
		return err
	}
	defer func() {
		_ = t.Cleanup()
	}()

	// Set permissions before writing data, in case the data is sensitive.
	if err := t.Chmod(perm); err != nil {
		return err
	}

	if _, err := t.Write(data); err != nil {
		return err
	}

	return t.CloseAtomicallyReplace()
}
