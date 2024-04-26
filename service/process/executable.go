package process

import (
	"crypto"
	"encoding/hex"
	"hash"
	"io"
	"os"
)

// GetExecHash returns the hash of the executable with the given algorithm.
func (p *Process) GetExecHash(algorithm string) (string, error) {
	sum, ok := p.ExecHashes[algorithm]
	if ok {
		return sum, nil
	}

	var hasher hash.Hash
	switch algorithm {
	case "md5":
		hasher = crypto.MD5.New()
	case "sha1":
		hasher = crypto.SHA1.New()
	case "sha256":
		hasher = crypto.SHA256.New()
	}

	file, err := os.Open(p.Path)
	if err != nil {
		return "", err
	}

	defer func() {
		_ = file.Close()
	}()

	_, err = io.Copy(hasher, file)
	if err != nil {
		return "", err
	}

	sum = hex.EncodeToString(hasher.Sum(nil))
	p.ExecHashes[algorithm] = sum
	return sum, nil
}
